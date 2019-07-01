package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/render"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2/clientcredentials"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Error struct {
	Reason     error
	StatusCode int
}

type Competitor struct {
	Title      *string
	Popularity int
	Images     []spotify.Image
}

type Matchup struct {
	TopCompetitor    *Competitor
	BottomCompetitor *Competitor
}

var client spotify.Client
var config *clientcredentials.Config

func Routes() *chi.Mux {
	router := chi.NewRouter()

	router.Use(
		render.SetContentType(render.ContentTypeJSON),
		//middleware.Logger,
		middleware.DefaultCompress,
		middleware.RedirectSlashes,
		middleware.Recoverer,
	)

	router.Get("/bracket/{competitorType}", CreateBracket)

	return router
}

func CreateBracket(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") == "http://localhost:3000" || r.Header.Get("Origin") == "http://174.16.217.76:3000" {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	}
	competitorType := chi.URLParam(r, "competitorType")
	size := r.FormValue("size")
	var sizeVal int
	var err error
	if size != "auto" {
		sizeVal, err = strconv.Atoi(size)
		if err != nil {
			fmt.Println(err)
			render.JSON(w, r, Error{Reason: err, StatusCode: 500})
			return
		}
		if math.Ceil(math.Log2(float64(sizeVal))) != math.Floor(math.Log2(float64(sizeVal))) {
			render.JSON(w, r, Error{Reason: errors.New("invalid sizeVal(must be a power of 2)"), StatusCode: 500})
			return
		}
	}
	var competitors []Competitor

	from := r.FormValue("from")
	market := spotify.CountryUSA
	if competitorType == "album" {
		albumType := spotify.AlbumType(spotify.AlbumTypeAlbum)
		albums, err := client.GetArtistAlbumsOpt(spotify.ID(from), &spotify.Options{Country: &market}, &albumType)
		if err != nil {
			fmt.Println(err)
			render.JSON(w, r, Error{Reason: err, StatusCode: 500})
			return
		}
		var albumIDs []spotify.ID
		//Fetch all album IDs
		for {
			for _, album := range albums.Albums {
				albumIDs = append(albumIDs, album.ID)
			}
			if albums.Next != "" {
				nextURL, err := url.Parse(albums.Next)
				if err != nil {
					fmt.Println(err)
					render.JSON(w, r, Error{Reason: err, StatusCode: 500})
					return
				}
				offset, err := strconv.Atoi(nextURL.Query().Get("offset"))
				if err != nil {
					fmt.Println(err)
					render.JSON(w, r, Error{Reason: err, StatusCode: 500})
					return
				}
				albums, err = client.GetArtistAlbumsOpt(spotify.ID(from), &spotify.Options{Offset: &offset, Country: &market}, &albumType)
				if err != nil {
					fmt.Println(err)
					render.JSON(w, r, Error{Reason: err, StatusCode: 500})
					return
				}
			} else {
				break
			}
		}
		for _, albumID := range albumIDs {
			fullAlbum, err := client.GetAlbum(albumID)
			if err != nil {
				fmt.Println(err)
				render.JSON(w, r, Error{Reason: err, StatusCode: 500})
				return
			}
			competitorToAdd := Competitor{
				Title:      &fullAlbum.Name,
				Popularity: fullAlbum.Popularity,
				Images:     fullAlbum.Images,
			}
			duplicate := false
			//Deal with deluxe, clean versions, etc. by popularity, then shorter name
			for i, competitor := range competitors {
				if competitor.Title == competitorToAdd.Title || competitor.Images[0] == competitorToAdd.Images[0] || (strings.HasPrefix(*competitor.Title, *competitorToAdd.Title) && (strings.Contains(*competitor.Title, "Deluxe") || strings.Contains(*competitor.Title, "Extended") || strings.Contains(*competitor.Title, "Exclusive"))) || (strings.HasPrefix(*competitorToAdd.Title, *competitor.Title) && (strings.Contains(*competitorToAdd.Title, "Deluxe") || strings.Contains(*competitorToAdd.Title, "Extended") || strings.Contains(*competitorToAdd.Title, "Exclusive"))) {
					duplicate = true
					if len(*competitor.Title) < len(*competitorToAdd.Title) {
						competitors[i] = competitor
					} else {
						competitors[i] = competitorToAdd
					}
				}
			}
			if !duplicate {
				competitors = append(competitors, competitorToAdd)
			}
		}
	} else if competitorType == "track" {
		tracks, err := client.GetPlaylistTracksOpt(spotify.ID(from), &spotify.Options{Country: &market}, "items(track(name,popularity,album(images))),next")
		if err != nil {
			fmt.Println(err)
			render.JSON(w, r, Error{Reason: err, StatusCode: 500})
			return
		}
		for ok := true; ok; ok = tracks.Next != "" {
			for _, track := range tracks.Tracks {
				track := track.Track
				competitors = append(competitors, Competitor{Title: &track.Name, Popularity: track.Popularity, Images: track.Album.Images})
			}
			if tracks.Next != "" {
				nextURL, err := url.Parse(tracks.Next)
				if err != nil {
					fmt.Println(err)
					render.JSON(w, r, Error{Reason: err, StatusCode: 500})
					return
				}
				offset, err := strconv.Atoi(nextURL.Query().Get("offset"))
				if err != nil {
					fmt.Println(err)
					render.JSON(w, r, Error{Reason: err, StatusCode: 500})
					return
				}
				tracks, err = client.GetPlaylistTracksOpt(spotify.ID(from), &spotify.Options{Offset: &offset, Country: &market}, "items(track(name,popularity,album(images))),next")
				if err != nil {
					fmt.Println(err)
					render.JSON(w, r, Error{Reason: err, StatusCode: 500})
					return
				}
			}
		}
	}

	//Construct bracket matchups from competitors
	sort.Slice(competitors, func(i int, j int) bool {
		if competitors[i].Popularity != competitors[j].Popularity && r.FormValue("seeded") == "true" {
			return competitors[i].Popularity > competitors[j].Popularity
		} else if r.FormValue("seeded") == "true" {
			return true
		} else {
			rand.Seed(time.Now().UnixNano())
			return rand.Float64() < 0.5
		}
	})
	if size == "auto" {
		sizeVal = len(competitors)
	}
	if len(competitors) > sizeVal {
		competitors = competitors[:sizeVal]
	}
	numCompetitors := sizeVal
	for ok := (numCompetitors / 2) > len(competitors); ok; ok = (numCompetitors / 2) > len(competitors) {
		numCompetitors = numCompetitors / 2
	}
	rounds := int(math.Ceil(math.Log2(float64(numCompetitors))))
	if numCompetitors < 2 {
		render.JSON(w, r, Error{Reason: errors.New("insufficient competitors"), StatusCode: 500})
	}
	matches := [][]int{
		{1, 2},
	}
	for round := 1; round < rounds; round++ {
		roundMatches := make([][]int, 0)
		sum := int(math.Pow(2, float64(round+1))) + 1
		for i := 0; i < len(matches); i++ {
			top := matches[i][0]
			bottom := sum - matches[i][0]
			roundMatches = append(roundMatches, []int{top, bottom})
			top = sum - matches[i][1]
			bottom = matches[i][1]
			roundMatches = append(roundMatches, []int{top, bottom})
		}
		matches = roundMatches
	}
	var matchups []Matchup
	for i := 0; i < len(matches); i++ {
		var top *Competitor
		var bottom *Competitor
		if matches[i][0] > len(competitors) {
			top = &Competitor{Title: nil}
		} else {
			top = &competitors[matches[i][0]-1]
		}
		if matches[i][1] > len(competitors) {
			bottom = &Competitor{Title: nil}
		} else {
			bottom = &competitors[matches[i][1]-1]
		}
		matchups = append(matchups, Matchup{TopCompetitor: top,
			BottomCompetitor: bottom})
	}
	render.JSON(w, r, matchups)
}

func main() {
	router := Routes()

	config = &clientcredentials.Config{
		ClientID:     os.Getenv("SPOTIFY_MADNESS_ID"),
		ClientSecret: os.Getenv("SPOTIFY_MADNESS_SECRET"),
		TokenURL:     spotify.TokenURL,
	}

	token, err := config.Token(context.Background())
	if err != nil {
		log.Fatalf("couldn't get token: %v", err)
	}

	client = spotify.Authenticator{}.NewClient(token)

	quit := make(chan bool)
	defer close(quit)

	go func() {
		for {
			select {
			case <-quit:
				return
			default:
				if token, err = client.Token(); err != nil {
					token, err = config.Token(context.Background())
					client = spotify.Authenticator{}.NewClient(token)
				}
			}
		}
	}()

	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal(http.ListenAndServe(":8000", router))
	} else {
		log.Fatal(http.ListenAndServe(":" + port, router))
	}
}
