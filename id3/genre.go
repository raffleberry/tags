package id3

import (
	"slices"
	"strings"

	"github.com/raffleberry/tags/tag"
)

// Genre returns the genre name for number n. Numbering starts at zero. The second result is false for numbers outside the list.
func Genre(n int) (string, bool) {
	if n < 0 || n >= len(tag.Genres) {
		return "", false
	}
	return tag.Genres[n], true
}

// ParseGenres resolves ID3v2 genre syntax into names. A TCON value holds a number, a name, or parenthesized numbers with an optional name. "(RX)" means "Remix". "(CR)" means "Cover". Values without a match are skipped.
func ParseGenres(values []string) []string {
	var genres []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			genres = append(genres, parseGenre(value)...)
		}
	}
	return genres
}

// parseGenre resolves one TCON value into genre names.
func parseGenre(value string) []string {
	// A bare number indexes the genre list from zero.
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
	// A trailing name is added unless it repeats a listed genre.
	if value != "" && !slices.Contains(genres, value) {
		genres = append(genres, value)
	}
	return genres
}

// genreToken resolves one parenthesized group or a bare genre number.
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

// numberOf returns the value of digit string s. It returns -1 for other input.
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
