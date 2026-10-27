package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

var genreList = []string{
	"Rock", "Electronic", "Pop", "Hip Hop", "Jazz", "Funk / Soul", "Classical",
	"Reggae", "Latin", "Folk, World, & Country", "Blues", "Stage & Screen", "Children's",
}

var genreAliases = map[string]string{
	"rock": "Rock", "alternative": "Rock", "alternative rock": "Rock", "indie": "Rock", "indie rock": "Rock",
	"punk": "Rock", "punk rock": "Rock", "metal": "Rock", "heavy metal": "Rock", "hard rock": "Rock",
	"grunge": "Rock", "classic rock": "Rock", "progressive rock": "Rock", "psychedelic rock": "Rock", "rock & roll": "Rock", "rock and roll": "Rock",
	"electronic": "Electronic", "electronica": "Electronic", "electro": "Electronic", "techno": "Electronic",
	"house": "Electronic", "deep house": "Electronic", "trance": "Electronic", "ambient": "Electronic",
	"edm": "Electronic", "dance": "Electronic", "drum and bass": "Electronic", "drum & bass": "Electronic",
	"dnb": "Electronic", "dubstep": "Electronic", "downtempo": "Electronic", "trip hop": "Electronic", "trip-hop": "Electronic", "idm": "Electronic", "synthwave": "Electronic",
	"pop": "Pop", "synth-pop": "Pop", "synthpop": "Pop", "indie pop": "Pop", "k-pop": "Pop", "dance-pop": "Pop", "schlager": "Pop", "chanson": "Pop",
	"hip hop": "Hip Hop", "hip-hop": "Hip Hop", "hiphop": "Hip Hop", "rap": "Hip Hop", "trap": "Hip Hop", "deutschrap": "Hip Hop",
	"jazz": "Jazz", "smooth jazz": "Jazz", "bebop": "Jazz", "swing": "Jazz", "fusion": "Jazz", "acid jazz": "Jazz",
	"funk / soul": "Funk / Soul", "funk": "Funk / Soul", "soul": "Funk / Soul", "r&b": "Funk / Soul", "rnb": "Funk / Soul",
	"r and b": "Funk / Soul", "rhythm & blues": "Funk / Soul", "disco": "Funk / Soul", "neo soul": "Funk / Soul", "gospel": "Funk / Soul",
	"classical": "Classical", "klassik": "Classical", "opera": "Classical", "baroque": "Classical", "orchestral": "Classical", "contemporary classical": "Classical",
	"reggae": "Reggae", "dub": "Reggae", "ska": "Reggae", "dancehall": "Reggae", "roots reggae": "Reggae",
	"latin": "Latin", "salsa": "Latin", "bossa nova": "Latin", "reggaeton": "Latin", "tango": "Latin", "cumbia": "Latin", "samba": "Latin",
	"folk, world, & country": "Folk, World, & Country", "folk": "Folk, World, & Country", "country": "Folk, World, & Country",
	"world": "Folk, World, & Country", "world music": "Folk, World, & Country", "americana": "Folk, World, & Country",
	"bluegrass": "Folk, World, & Country", "singer-songwriter": "Folk, World, & Country", "celtic": "Folk, World, & Country", "volksmusik": "Folk, World, & Country",
	"blues": "Blues", "delta blues": "Blues", "blues rock": "Blues", "chicago blues": "Blues",
	"stage & screen": "Stage & Screen", "soundtrack": "Stage & Screen", "score": "Stage & Screen", "film score": "Stage & Screen",
	"musical": "Stage & Screen", "video game music": "Stage & Screen", "ost": "Stage & Screen", "filmmusik": "Stage & Screen",
	"children's": "Children's", "childrens": "Children's", "children": "Children's", "kids": "Children's", "kinderlieder": "Children's", "hörspiel": "Children's",
}

func canonGenre(raw string) string {
	s := strings.Join(strings.Fields(raw), " ")
	if s == "" {
		return ""
	}
	if g, ok := genreAliases[strings.ToLower(s)]; ok {
		return g
	}
	return s
}

func splitGenres(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ';' })
}

type genreAlbum struct {
	Genre  string `json:"genre"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
}

func albumGenres(raw json.RawMessage) []string {
	var al genreAlbum
	if json.Unmarshal(raw, &al) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		for _, part := range splitGenres(v) {
			g := canonGenre(part)
			if g != "" && !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	for _, g := range al.Genres {
		add(g.Name)
	}
	if len(out) == 0 {
		add(al.Genre)
	}
	return out
}

func genreFilter(r *http.Request) map[string]bool {
	want := map[string]bool{}
	for _, v := range r.URL.Query()["genre"] {
		if g := canonGenre(v); g != "" {
			want[g] = true
		}
	}
	return want
}

func (a *App) handleAlbums(w http.ResponseWriter, r *http.Request, user string) {
	want := genreFilter(r)
	if len(want) == 0 {
		a.musicJSON("getAlbumList2.view", "albumList2", albumListParams)(w, r, user)
		return
	}
	q, ok := albumListParams(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	limit, _ := strconv.Atoi(q.Get("size"))
	q.Set("size", "500")
	q.Set("offset", "0")
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, err := a.nav.raw(r.Context(), "getAlbumList2.view", "albumList2", creds, q)
	if err != nil {
		writeMusicError(w, "getAlbumList2.view", err)
		return
	}
	var list struct {
		Album []json.RawMessage `json:"album"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		writeMusicError(w, "getAlbumList2.view", err)
		return
	}
	out := []json.RawMessage{}
	for _, al := range list.Album {
		if len(out) >= limit {
			break
		}
		for _, g := range albumGenres(al) {
			if want[g] {
				out = append(out, al)
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"album": out})
}

type genreCount struct {
	Name   string `json:"name"`
	Albums int    `json:"albums"`
}

func (a *App) handleGenres(w http.ResponseWriter, r *http.Request, user string) {
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, err := a.nav.raw(r.Context(), "getGenres.view", "genres", creds, nil)
	if err != nil {
		writeMusicError(w, "getGenres.view", err)
		return
	}
	var data struct {
		Genre []struct {
			Value      string `json:"value"`
			AlbumCount int    `json:"albumCount"`
		} `json:"genre"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		writeMusicError(w, "getGenres.view", err)
		return
	}
	counts := map[string]int{}
	for _, g := range data.Genre {
		for _, part := range splitGenres(g.Value) {
			if c := canonGenre(part); c != "" && g.AlbumCount > 0 {
				counts[c] += g.AlbumCount
			}
		}
	}
	out := make([]genreCount, 0, len(counts))
	for name, n := range counts {
		out = append(out, genreCount{name, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Albums != out[j].Albums {
			return out[i].Albums > out[j].Albums
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, http.StatusOK, map[string]any{"genres": out, "list": genreList})
}
