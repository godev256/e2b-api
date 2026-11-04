-- Token bucket in one atomic step.
--
-- KEYS[1]  bucket key (a hash holding the current tokens and a timestamp)
-- ARGV[1]  refill rate, tokens per second
-- ARGV[2]  bucket capacity (burst)
-- ARGV[3]  tokens this request costs
--
-- The clock is Redis TIME, not the caller's, so API instances with skewed
-- clocks still agree. Returns {allowed, remaining, retry_after_ms, reset_after_ms}.

local rate  = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local cost  = tonumber(ARGV[3])

local time   = redis.call('TIME')
local now_ms = time[1] * 1000 + math.floor(time[2] / 1000)

local state  = redis.call('HMGET', KEYS[1], 'tokens', 'updated_ms')
local tokens = tonumber(state[1])
local updated_ms = tonumber(state[2])
if tokens == nil then
  tokens = burst
  updated_ms = now_ms
end

local elapsed_ms = math.max(0, now_ms - updated_ms)
tokens = math.min(burst, tokens + elapsed_ms * rate / 1000)

local allowed = 0
local retry_after_ms = 0
if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
else
  retry_after_ms = math.ceil((cost - tokens) * 1000 / rate)
end

local reset_after_ms = math.ceil((burst - tokens) * 1000 / rate)

redis.call('HSET', KEYS[1], 'tokens', tokens, 'updated_ms', now_ms)
-- An idle bucket is full again after reset_after_ms, so it can be forgotten then.
redis.call('PEXPIRE', KEYS[1], math.max(reset_after_ms, 1000))

return {allowed, math.floor(tokens), retry_after_ms, reset_after_ms}
