package spotifyhandler

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/zmb3/spotify"
)

// redirectURI is the OAuth redirect URI for the application.
// You must register an application at Spotify's developer portal
// and enter this value.
const redirectURI = "http://localhost:8080/callback"
const publicPlaylistName = "Pauls Liked Songs"

var (
	auth      = spotify.NewAuthenticator(redirectURI, spotify.ScopeUserReadPrivate, spotify.ScopePlaylistReadPrivate, spotify.ScopeUserLibraryRead, spotify.ScopePlaylistModifyPublic)
	ch        = make(chan *spotify.Client)
	state     = "abc123"
	edmGenres = [...]string{"house", "deep", "edm", "groove", "dance", "electronic", "big room", "progressive", "dubstep", "brazilian edm", "bass"}
)

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}
	// use the token to get an authenticated client
	client := auth.NewClient(tok)
	fmt.Fprintf(w, "Login Completed!")
	ch <- &client
}

func getLikedSongsPlaylistID(client *spotify.Client) (playlistID spotify.ID) {
	if playlists, err := client.CurrentUsersPlaylists(); err == nil {
		for _, playlist := range playlists.Playlists {
			if playlist.Name == publicPlaylistName {
				playlistID = playlist.ID
			}
		}
	} else {
		fmt.Printf("err: %v", err)
	}
	return
}

func getLikedSongIds(client *spotify.Client) []spotify.ID {
	likedSongs, err := client.CurrentUsersTracks()
	likedSongIds := make([]spotify.ID, likedSongs.Total)
	if err != nil {
		log.Fatal(err)
	}
	for page := 1; ; page++ {
		for idx, track := range likedSongs.Tracks {
			likedSongIds[likedSongs.Offset+idx] = track.ID
		}
		if err := client.NextPage(likedSongs); err == spotify.ErrNoMorePages {
			break
		} else if err != nil {
			log.Fatal(err)
		}
	}
	return likedSongIds
}

func getPlaylistSongIds(client *spotify.Client, playlistID spotify.ID) []spotify.ID {
	playlistTracks, err := client.GetPlaylistTracks(playlistID)
	playlistSongIds := make([]spotify.ID, playlistTracks.Total)
	if err != nil {
		log.Fatal(err)
	}
	for page := 1; ; page++ {
		for idx, track := range playlistTracks.Tracks {
			playlistSongIds[playlistTracks.Offset+idx] = track.Track.ID
		}
		if err := client.NextPage(playlistTracks); err == spotify.ErrNoMorePages {
			break
		} else if err != nil {
			log.Fatal(err)
		}
	}
	return playlistSongIds
}

func contains(playlist []spotify.ID, track spotify.ID) bool {
	for _, pt := range playlist {
		if pt == track {
			return true
		}
	}
	return false

}

func isArtistEdmGenre(artist *spotify.FullArtist) bool {
	for _, genre := range edmGenres {
		for _, artistGenre := range artist.Genres {
			if genre == artistGenre {
				return true
			}
		}
	}
	return false
}

func syncPublicPlaylistWithLikedSongs(client *spotify.Client, playlistID spotify.ID, playlistSongs []spotify.ID, likedSongs []spotify.ID) []spotify.ID {
	fmt.Println("Begin Syncing")
	var addedLikedSongs []spotify.ID
	for _, likedSong := range likedSongs {
		if contains(playlistSongs, likedSong) == false {
			_, err := client.AddTracksToPlaylist(playlistID, likedSong)
			if err != nil {
				fmt.Printf("Error adding song: %v \n", err)
			}
			addedLikedSongs = append(addedLikedSongs, likedSong)
		}
	}

	for _, song := range playlistSongs {
		if contains(likedSongs, song) == false {
			_, err := client.RemoveTracksFromPlaylist(playlistID, song)
			if err != nil {
				fmt.Printf("Error removing song: %v \n", err)
			}
		}
	}
	fmt.Println("Sync Complete")
	return addedLikedSongs
}

func mapAndFilterEdmSongs(client *spotify.Client, tracks []spotify.ID) []spotify.FullTrack {
	var edmSongs []spotify.FullTrack
	for _, trackId := range tracks {
		track, err := client.GetTrack(trackId)
		if err != nil {
			fmt.Printf("Error finding song")
		}
		fullArtist, err := client.GetArtist(track.Artists[0].ID)
		if err != nil {
			fmt.Printf("Error finding song")
		}
		if isArtistEdmGenre(fullArtist) {
			edmSongs = append(edmSongs, *track)
		}
	}
	return edmSongs
}

func Serve(myChan chan []spotify.FullTrack) {
	// first start an HTTP server
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	go http.ListenAndServe(":8080", nil)
	url := auth.AuthURL(state)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// wait for auth to complete
	client := <-ch
	for {
		playlistID := getLikedSongsPlaylistID(client)
		playlistSongs := getPlaylistSongIds(client, playlistID)
		likedSongs := getLikedSongIds(client)
		addedLikedSongs := syncPublicPlaylistWithLikedSongs(client, playlistID, playlistSongs, likedSongs)
		newLikedEdmSongs := mapAndFilterEdmSongs(client, addedLikedSongs)
		myChan <- newLikedEdmSongs
		time.Sleep(30 * time.Second)
	}
}
