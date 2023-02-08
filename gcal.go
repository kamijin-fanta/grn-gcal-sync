package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
)

const authTimeout = 3 * time.Minute

type GcalClient struct {
	service *calendar.Service
}

func NewGcalClient(interactive bool, tokenPath string, loopbackPort string) (*GcalClient, error) {
	b, err := ioutil.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, calendar.CalendarReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	// read write access
	config.Scopes = append(config.Scopes, "https://www.googleapis.com/auth/calendar.events")
	client := getClient(config, interactive, tokenPath, loopbackPort)

	srv, err := calendar.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}

	return &GcalClient{
		service: srv,
	}, nil
}

func (g *GcalClient) listOfCalender() {
	res, err := g.service.CalendarList.List().Do()
	fmt.Printf("%v %v\n", len(res.Items), err)
	for _, cal := range res.Items {
		fmt.Printf(" %v %v\n", cal.Summary, cal.Id)
	}
}

func (g *GcalClient) getEvents(start, end time.Time, calId string) (*calendar.Events, error) {
	startStr := start.Format(time.RFC3339)
	endStr := end.Format(time.RFC3339)
	events, err := g.service.Events.List(calId).
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(startStr).
		TimeMax(endStr).
		MaxResults(2500).
		OrderBy("startTime").Do()
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (g *GcalClient) todo() {}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config, interactive bool, tokenPath string, loopbackPort string) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tok, err := tokenFromFile(tokenPath)
	if err != nil {
		if interactive {
			tok = getTokenFromWeb(config, loopbackPort)
			saveToken(tokenPath, tok)
		} else {
			panic(fmt.Errorf("not found token %w", err))
		}
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config, loopbackPort string) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	authCode, err := waitLoopbackRequest(config, loopbackPort, authTimeout)
	if err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func waitLoopbackRequest(config *oauth2.Config, loopbackPort string, timeout time.Duration) (authCode string, e error) {
	m := http.NewServeMux()
	s := http.Server{Addr: ":" + loopbackPort, Handler: m}

	ctx, cancel := context.WithTimeout(context.TODO(), timeout)

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code, ok := r.URL.Query()["code"]
		if !ok {
			return
		}

		authCode = code[0]
		cancel()
	})

	// log.Println("Starting loopback listener with " + s.Addr)
	go func() {
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			e = ctx.Err()
		}
		s.Shutdown(ctx)
	}

	// log.Println("Loopback listener closed")
	return
}
