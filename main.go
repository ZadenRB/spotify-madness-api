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
	Title      string
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

	router.Get("/bracket/{competitorType}/{selectionType}", CreateBracket)

	return router
}

func CreateBracket(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://174.16.217.76:3000")
	competitorType := chi.URLParam(r, "competitorType")
	selectionType := chi.URLParam(r, "selectionType")
	size, err := strconv.Atoi(r.FormValue("size"))
	if err != nil {
		fmt.Println(err)
		render.JSON(w, r, Error{Reason: err, StatusCode: 500})
		return
	}
	var competitors []Competitor

	if selectionType == "auto" {
		from := r.FormValue("from")
		if err != nil {
			fmt.Println(err)
			render.JSON(w, r, Error{Reason: err, StatusCode: 500})
			return
		}
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
					Title:      fullAlbum.Name,
					Popularity: fullAlbum.Popularity,
					Images:     fullAlbum.Images,
				}
				duplicate := false
				//Deal with deluxe, clean versions, etc. by popularity, then shorter name
				for i, competitor := range competitors {
					if competitor.Title == competitorToAdd.Title || competitor.Images[0] == competitorToAdd.Images[0] || (strings.HasPrefix(competitor.Title, competitorToAdd.Title) && (strings.Contains(competitor.Title, "Deluxe") || strings.Contains(competitor.Title, "Extended") || strings.Contains(competitor.Title, "Exclusive"))) || (strings.HasPrefix(competitorToAdd.Title, competitor.Title) && (strings.Contains(competitorToAdd.Title, "Deluxe") || strings.Contains(competitorToAdd.Title, "Extended") || strings.Contains(competitorToAdd.Title, "Exclusive"))) {
						duplicate = true
						if competitorToAdd.Popularity > competitor.Popularity {
							competitors[i] = competitorToAdd
						} else if competitorToAdd.Popularity == competitor.Popularity {
							if len(competitorToAdd.Title) < len(competitor.Title) {
								competitors[i] = competitorToAdd
							} else if len(competitorToAdd.Title) == len(competitor.Title) {
								rand.Seed(time.Now().UnixNano())
								if rand.Float32() < 0.5 {
									competitors[i] = competitorToAdd
								}
							}
						}
					}
				}
				if !duplicate {
					competitors = append(competitors, competitorToAdd)
				}
			}
		} else if competitorType == "track" {
			tracks, err := client.GetPlaylistTracksOpt(spotify.ID(from), nil, "items(track(name,popularity,album(images))),next")
			if err != nil {
				fmt.Println(err)
				render.JSON(w, r, Error{Reason: err, StatusCode: 500})
				return
			}
			for ok := true; ok; ok = tracks.Next != "" {
				for _, track := range tracks.Tracks {
					track := track.Track
					competitors = append(competitors, Competitor{Title: track.Name, Popularity: track.Popularity, Images: track.Album.Images})
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
					tracks, err = client.GetPlaylistTracksOpt(spotify.ID(from), &spotify.Options{Offset: &offset, Country: &market}, "tracks.items(track(name,popularity,album(images))),tracks.next")
					if err != nil {
						fmt.Println(err)
						render.JSON(w, r, Error{Reason: err, StatusCode: 500})
						return
					}
				}
			}
		}
	}

	//Construct bracket matchups from competitors
	sort.Slice(competitors, func(i int, j int) bool {
		if competitors[i].Popularity != competitors[j].Popularity && r.FormValue("seeded") == "true" {
			return competitors[i].Popularity > competitors[j].Popularity
		} else {
			return true
		}
	})
	if len(competitors) > size {
		competitors = competitors[:size]
	}
	numCompetitors := size
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
			top = &Competitor{Title: "Bye"}
		} else {
			top = &competitors[matches[i][0]-1]
		}
		if matches[i][1] > len(competitors) {
			bottom = &Competitor{Title: "Bye"}
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

	log.Fatal(http.ListenAndServe(":8000", router))

	quit <- true
}
