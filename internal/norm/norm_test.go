package norm

import "testing"

func TestKey(t *testing.T) {
	same := [][2][2]string{
		{{"The Beatles", "Let It Be - Remastered 2009"}, {"Beatles", "Let It Be"}},
		{{"Beyoncé", "Halo (Live)"}, {"Beyonce", "Halo"}},
		{{"Daft Punk", "Get Lucky (feat. Pharrell Williams)"}, {"daft punk", "get lucky"}},
		{{"Simon & Garfunkel", "The Boxer"}, {"Simon and Garfunkel", "The Boxer"}},
		{{"Mina", "Se telefonando - 2001 Remaster"}, {"Mina", "Se Telefonando"}},
	}
	for _, c := range same {
		a, b := Key(c[0][0], c[0][1]), Key(c[1][0], c[1][1])
		if a != b {
			t.Errorf("chiavi diverse: %q vs %q", a, b)
		}
	}
	if Key("Radiohead", "Creep") == Key("Radiohead", "Karma Police") {
		t.Error("brani diversi con stessa chiave")
	}
}

func TestArtistMatch(t *testing.T) {
	if !ArtistMatch("Beyonce", []string{"Jay-Z", "Beyoncé"}) {
		t.Error("atteso match su artista secondario")
	}
	if !ArtistMatch("Florence + The Machine", []string{"Florence + the Machine"}) {
		t.Error("atteso match case-insensitive")
	}
	if ArtistMatch("Low", []string{"Lowell"}) {
		t.Error("nomi corti non devono matchare per sottostringa")
	}
	if ArtistMatch("Slowdive", []string{"Ride"}) {
		t.Error("match inatteso")
	}
}

func TestTitleMatch(t *testing.T) {
	if !TitleMatch("Alison", "Alison - 2005 Remaster") {
		t.Error("atteso match con remaster")
	}
	if !TitleMatch("When the Sun Hits", "When The Sun Hits") {
		t.Error("atteso match case-insensitive")
	}
	if TitleMatch("Alison", "Alison's Song") {
		t.Error("match inatteso")
	}
}
