package common

import (
	"math/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func Try(f func() error) error {
	var finalError error
	func() {
		defer func() {
			if err := recover(); err != nil {
				finalError = fmt.Errorf("Runtime Error: %v", err)
			}
		}()
		finalError = f()
	}()

	return finalError
}

func GenerateID() string {
	// Generate a random byte slice
	keyBytes := make([]byte, 32)
	_, err := rand.Read(keyBytes)
	if err != nil {
		return GenerateID()
	}
	// Convert the byte slice to a hex-encoded string
	return hex.EncodeToString(keyBytes)
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func GetRandomNumber(n int) int {
    return rng.Intn(n)

}