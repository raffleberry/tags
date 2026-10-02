package id3

import (
	"slices"
	"strings"

	"github.com/raffleberry/tags/tag"
)

// Genre returns the name of the genre with the given number. The numbering is
// the one ID3 itself uses, where zero is the first entry, so both the genre byte
// of an ID3v1 tag and a number inside a TCON frame index the list directly.
//
// The second result is false for a number outside the list. Note that this is
// not the numbering iTunes uses in its "gnre" atom, which counts from one.
func Genre(n int) (string, bool) {
	if n < 0 || n >= len(tag.Genres) {
		return "", false
	}
	return tag.Genres[n], true
}

// ParseGenres resolves the genre syntax ID3v2 allows into plain names. A TCON
// frame holds either a bare number, a bare name, or one or more numbers in
// parentheses followed by an optional clarifying name, as in "(17)", "(17)Britpop"
// or "(17)(20)". The tokens "(RX)" and "(CR)" mean "Remix" and "Cover".
//
// A value that resolves to nothing is skipped, so the result may be empty.
func ParseGenres(values []string) []string {
	var genres []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			genres = append(genres, parseGenre(value)...)
		}
	}
	return genres
}

// parseGenre resolves one TCON value, which may hold several genres.
func parseGenre(value string) []string {
	// The number a tagger of the nineties wrote is the genre byte of an ID3v1
	// tag, so it indexes the list from zero.
	if name, ok := Genre(numberOf(value)); ok {
		return []string{name}
	}
	if name, ok := genreToken(value); ok {
		return []string{name}
	}

	var genres []string
	for strings.HasPrefix(value, "(") {
		end := strings.IndexByte(value, ')')
		if end < 0 {
			break
		}
		if name, ok := genreToken(value[1:end]); ok {
			genres = append(genres, name)
		}
		value = strings.TrimLeft(value[end+1:], " ")
	}
	// A name the tagger spelled out wins over the numbers it came with, unless
	// it merely repeats one of them.
	if value != "" && !slices.Contains(genres, value) {
		genres = append(genres, value)
	}
	return genres
}

// genreToken resolves the inside of one "(...)" group, or a bare value that is
// entirely a genre number.
func genreToken(token string) (string, bool) {
	switch token {
	case "RX":
		return "Remix", true
	case "CR":
		return "Cover", true
	default:
		return Genre(numberOf(token))
	}
}

// numberOf returns the value of a string of digits, or -1 when the string holds
// something else.
func numberOf(s string) int {
	if s == "" {
		return -1
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return -1
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
