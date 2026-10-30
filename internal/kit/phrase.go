package kit

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// PhraseWords is the generated phrase's length: five words from the EFF
// large wordlist, about 64.6 bits (CTO-10 point 7).
const PhraseWords = 5

// effListSize is the EFF large wordlist's length.
const effListSize = 7776

// effWords is the EFF large wordlist, embedded with its attribution in the
// license notices. Empty until the list is added to the repository (BV-118
// point 3): a generated phrase is then refused, never made from another list.
var effWords []string

// ErrNoWordlist is a generated phrase asked of a build without the list.
var ErrNoWordlist = errors.New("kit: this build has no word list for generated phrases yet")

// GeneratePhrase is five words of the EFF large wordlist, each chosen with
// crypto/rand, separated by spaces.
func GeneratePhrase() (string, error) { return generatePhrase(effWords) }

func generatePhrase(words []string) (string, error) {
	if len(words) != effListSize {
		return "", ErrNoWordlist
	}
	picked := make([]string, PhraseWords)
	for i := range picked {
		n, err := rand.Int(rand.Reader, big.NewInt(effListSize))
		if err != nil {
			return "", fmt.Errorf("kit: pick a word: %w", err)
		}
		picked[i] = words[n.Int64()]
	}
	return strings.Join(picked, " "), nil
}
