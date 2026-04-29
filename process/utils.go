package process

import (
	"math/rand"
	"strings"
	"time"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var randGen *rand.Rand

func init() {
	randGen = rand.New(rand.NewSource(time.Now().Unix()))
}

func RandString(n int) string {
	sb := strings.Builder{}

	for range n {
		index := randGen.Intn(len(letterBytes))
		c := letterBytes[index]
		sb.WriteByte(c)
	}
	return sb.String()
}
