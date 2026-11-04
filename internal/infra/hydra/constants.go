package hydra

const (
	// maxJWKSBytes bounds the key set we are willing to read.
	maxJWKSBytes = 1 << 20

	signingAlgorithm = "RS256"
)
