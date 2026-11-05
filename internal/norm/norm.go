// Package norm normalizza nomi di artisti e brani per confronti robusti.
package norm

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	bracketRe = regexp.MustCompile(`[\(\[][^\)\]]*[\)\]]`)
	// " - Remastered 2011", " - Live", " - From \"Film\"", ecc.
	suffixRe = regexp.MustCompile(`\s+[-–—]\s+.*$`)
	featRe   = regexp.MustCompile(`\b(feat\.?|ft\.?|featuring)\s.*$`)
	spaceRe  = regexp.MustCompile(`\s+`)
)

func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	r, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return r
}

func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '\'' || r == '’':
			// "Alison's" -> "alisons": l'apostrofo non separa parole
		case r == '&':
			b.WriteString(" and ")
		default:
			b.WriteRune(' ')
		}
	}
	return strings.TrimSpace(spaceRe.ReplaceAllString(b.String(), " "))
}

// Title normalizza il titolo di un brano rimuovendo versioni e featuring.
func Title(s string) string {
	s = strings.ToLower(fold(s))
	s = bracketRe.ReplaceAllString(s, " ")
	s = suffixRe.ReplaceAllString(s, "")
	s = featRe.ReplaceAllString(s, "")
	return clean(s)
}

// Artist normalizza il nome di un artista ("The Beatles" == "Beatles").
func Artist(s string) string {
	s = clean(strings.ToLower(fold(s)))
	return strings.TrimPrefix(s, "the ")
}

// Key identifica un brano indipendentemente da versioni/remaster.
func Key(artist, title string) string {
	return Artist(artist) + "|" + Title(title)
}

// ArtistMatch confronta un artista suggerito con gli artisti di un brano.
func ArtistMatch(want string, have []string) bool {
	w := Artist(want)
	if w == "" {
		return false
	}
	for _, h := range have {
		a := Artist(h)
		if a == w || (len(w) >= 4 && strings.Contains(a, w)) || (len(a) >= 4 && strings.Contains(w, a)) {
			return true
		}
	}
	return false
}

// TitleMatch confronta titoli normalizzati.
func TitleMatch(want, have string) bool {
	w := Title(want)
	return w != "" && w == Title(have)
}
