package kit

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"math/big"
	"strings"
)

// PhraseWords is the generated phrase's length: five words from the EFF
// large wordlist, about 64.6 bits (CTO-10 point 7).
const PhraseWords = 5

// effList is the EFF Large Wordlist for Passphrases, unchanged (BV-120;
// attribution in THIRD_PARTY_NOTICES.md): 7,776 lines of five dice digits, a
// tab and a word.
//
//go:embed eff_large_wordlist.txt
var effList string

// effWords are effList's words, in order.
var effWords = parseWordlist(effList)

func parseWordlist(list string) []string {
	var words []string
	for _, line := range strings.Split(strings.TrimRight(list, "\n"), "\n") {
		_, word, _ := strings.Cut(line, "\t")
		words = append(words, word)
	}
	return words
}

// GeneratePhrase is five words of the EFF large wordlist, each chosen with
// crypto/rand, separated by spaces.
func GeneratePhrase() (string, error) {
	picked := make([]string, PhraseWords)
	for i := range picked {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(effWords))))
		if err != nil {
			return "", fmt.Errorf("kit: pick a word: %w", err)
		}
		picked[i] = effWords[n.Int64()]
	}
	return strings.Join(picked, " "), nil
}
