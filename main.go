package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/joho/godotenv"
	"github.com/urfave/cli/v2"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
)

func main() {
	godotenv.Load()

	app := cli.NewApp()
	app.Name = "grn-gcal-sync"
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "grn-user-id",
			Usage:   "garoon target user id",
			EnvVars: []string{"GAROON_USER_ID"},
		},
		&cli.StringFlag{
			Name:    "grn-link-base",
			Usage:   "garoon access base url",
			EnvVars: []string{"GAROON_LINK_BASE"},
		},
		&cli.StringFlag{
			Name:    "grn-subdomain",
			Usage:   "garoon cloud version tenant sub-domain",
			EnvVars: []string{"GAROON_SUBDOMAIN"},
		},
		&cli.StringFlag{
			Name:    "gcal-token-path",
			Usage:   "google calendear oauth token file",
			Value:   "data/token.json",
			EnvVars: []string{"GCAL_TOKEN_PATH"},
		},
		&cli.StringFlag{
			Name:    "gcal-id",
			Usage:   "target calendar id",
			EnvVars: []string{"GCAL_ID"},
		},
		&cli.StringFlag{
			Name:    "gcal-auth-loopback-port",
			Usage:   "port used for loopback IP address flow",
			Value:   "31080",
			EnvVars: []string{"GCAL_AUTH_LOOPBACK_PORT"},
		},

		&cli.StringFlag{
			Name:    "garoon-token-path",
			Usage:   "garoon oauth token file",
			Value:   "data/tokengrn.json",
			EnvVars: []string{"GAROON_TOKEN_PATH"},
		},
		&cli.StringFlag{
			Name:    "garoon-oauth2-client-id",
			Usage:   "garoon oauth2 client",
			EnvVars: []string{"GAROON_OAUTH2_CLIENT_ID"},
		},
		&cli.StringFlag{
			Name:    "garoon-oauth2-client-secret",
			Usage:   "garoon oauth2 client",
			EnvVars: []string{"GAROON_OAUTH2_CLIENT_SECRET"},
		},
		&cli.StringFlag{
			Name:    "garoon-oauth2-client-authorization",
			Usage:   "garoon oauth2 client",
			EnvVars: []string{"GAROON_OAUTH2_CLIENT_AUTHORIZATION"},
		},
		&cli.StringFlag{
			Name:    "garoon-oauth2-client-token",
			Usage:   "garoon oauth2 client",
			EnvVars: []string{"GAROON_OAUTH2_CLIENT_TOKEN"},
		},
		&cli.StringFlag{
			Name:    "garoon-oauth2-callback",
			Usage:   "garoon oauth2 callback uri",
			EnvVars: []string{"GAROON_OAUTH2_CALLBACK"},
		},

		&cli.BoolFlag{
			Name:  "no-interactive",
			Usage: "target calendar id",
		},
	}

	app.Commands = []*cli.Command{
		{
			Name:  "sync",
			Usage: "sync",
			Flags: []cli.Flag{},
			Action: func(c *cli.Context) error {
				garoonOauth2Config := oauth2.Config{
					ClientID:     c.String("garoon-oauth2-client-id"),
					ClientSecret: c.String("garoon-oauth2-client-secret"),
					Endpoint: oauth2.Endpoint{
						AuthURL:  c.String("garoon-oauth2-client-authorization"),
						TokenURL: c.String("garoon-oauth2-client-token"),
					},
					RedirectURL: c.String("garoon-oauth2-callback"),
					Scopes:      []string{"g:schedule:read"},
				}
				authenticator := &Oauth2LocalAuthenticator{
					config: garoonOauth2Config,
				}
				ctx := context.Background()
				log.Printf("oauth2 start %v", authenticator)

				var token *oauth2.Token
				tokenPath := c.String("garoon-token-path")
				if buff, err := os.ReadFile(tokenPath); err == nil {
					t := oauth2.Token{}
					err := json.Unmarshal(buff, &t)
					if err != nil {
						panic(err)
					}
					token = &t
				}

				if token == nil {
					if c.Bool("no-interactive") {
						return fmt.Errorf("inlivad accesskey for garoon")
					}
					resToken, errr := authenticator.Start(ctx)
					if errr != nil {
						return errr
					}
					jsonbuff, err := json.Marshal(resToken)
					if err != nil {
						panic(err)
					}
					err = os.WriteFile(tokenPath, jsonbuff, os.ModePerm)
					if err != nil {
						panic(err)
					}

					token = resToken
				}

				tokenSource := oauth2.StaticTokenSource(token)

				grn := NewGrnClient()
				grn.baseUrl = "https://" + c.String("grn-subdomain") + ".cybozu.com/g"
				grn.hc = oauth2.NewClient(ctx, tokenSource)
				gcal, err := NewGcalClient(!c.Bool("no-interactive"), c.String("gcal-token-path"), c.String("gcal-auth-loopback-port"))
				if err != nil {
					panic(err)
				}

				now := time.Now()
				zone := time.FixedZone("Asia/Tokyo", 9*60*60)
				start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, zone) // 月初
				end := time.Date(now.Year(), now.Month()+2, 0, 0, 0, 0, 0, zone) // 来月末

				//start, err = time.Parse(time.RFC3339, "2020-06-14T00:00:00+09:00")
				//if err != nil {
				//	panic(err)
				//}
				//end, err = time.Parse(time.RFC3339, "2020-06-16T00:00:00+09:00")
				//if err != nil {
				//	panic(err)
				//}

				grnEvents, err := grn.EventsByUser(start, end, c.String("grn-user-id"))
				if err != nil {
					panic(err)
				}

				calId := c.String("gcal-id")
				gcalTodayEvents, err := gcal.getEvents(start, end, calId)
				if err != nil {
					panic(err)
				}

				// gcal側に有るけどgrn側に無い予定を探すためのmap
				remainGcalEvent := make(map[string]*calendar.Event)
				for _, event := range gcalTodayEvents.Items {
					remainGcalEvent[event.Id] = event
				}

				for _, srcEvent := range grnEvents {
					if isIgnoreTitle(srcEvent.Subject) {
						fmt.Printf("Title ignore %v\n", srcEvent.Subject)
						continue
					}

					var foundGcalEvent *calendar.Event
					for _, dstEvent := range remainGcalEvent {
						if findSyncId(dstEvent.Description) == formatSyncId(srcEvent.ID, srcEvent.RepeatID) {
							foundGcalEvent = dstEvent
							delete(remainGcalEvent, dstEvent.Id)
							break
						}
					}

					start := new(calendar.EventDateTime)
					end := new(calendar.EventDateTime)
					if srcEvent.IsAllDay {
						format := "2006-01-02"
						start.Date = srcEvent.Start.DateTime.Format(format)
						end.Date = srcEvent.End.DateTime.Add(24 * time.Hour).Format(format) // google カレンダーの終了日形式に合わせるため+1日
					} else {
						start.DateTime = srcEvent.Start.DateTime.Format(time.RFC3339)
						end.DateTime = srcEvent.End.DateTime.Format(time.RFC3339)
					}

					url := fmt.Sprintf(`%s/schedule/view?event=%d`, c.String("grn-link-base"), srcEvent.ID)

					attendees := fmt.Sprintf("参加者(%d名): ", len(srcEvent.Attendees))
					for index, attendee := range srcEvent.Attendees {
						attendees += attendee.Name + "  "
						if index > 10 {
							attendees += "...その他省略"
							break
						}
					}
					syncId := formatSyncId(srcEvent.ID, srcEvent.RepeatID)
					description := fmt.Sprintf("%s\n%s\n\n----------\n%s\n----------\n%s", url, attendees, srcEvent.Notes, syncId)

					exceptEvent := &calendar.Event{
						Summary:     srcEvent.Subject,
						Description: description,
						Start:       start,
						End:         end,
					}

					if foundGcalEvent == nil {
						// insert new events
						fmt.Printf("Insert new event %s\n", srcEvent.Subject)
						_, err := gcal.service.Events.Insert(calId, exceptEvent).Do()
						if err != nil {
							panic(err)
						}
					} else {
						// update events
						diff := cmp.Diff(exceptEvent, foundGcalEvent, cmpopts.IgnoreFields(*exceptEvent, "Created", "Creator", "Etag", "ICalUID", "Id", "HtmlLink", "Status", "Updated", "Reminders", "Organizer", "Kind", "Sequence", "Start.TimeZone", "End.TimeZone"))
						if diff != "" {
							fmt.Printf("Update event %s\n%s\n\n", srcEvent.Subject, diff)
							maxRetries := 5
							retries := 0
							var lasterr error
							for {
								_, lasterr := gcal.service.Events.Update(calId, foundGcalEvent.Id, exceptEvent).Do()
								if lasterr != nil {
									if retries >= maxRetries {
										break
									}
									fmt.Printf("Error in gcal.service.Events.Update:%v. retrying %d/%d\n", lasterr, retries, maxRetries)
									time.Sleep(time.Duration(2<<retries) * time.Second)

									retries++
									continue
								}
								break
							}
							if lasterr != nil {
								panic(lasterr)
							}
						} else {
							fmt.Printf("Ignore event %s\n", srcEvent.Subject)
						}
					}
				}

				for _, dstEvent := range remainGcalEvent {
					fmt.Printf("Delete event %s\n", dstEvent.Summary)
					err := gcal.service.Events.Delete(calId, dstEvent.Id).Do()
					if err != nil {
						panic(err)
					}
				}

				return nil
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		panic(err)
	}
}

func findSyncId(description string) string {
	rows := strings.Split(description, "\n")
	for _, row := range rows {
		if strings.HasPrefix(row, "sync-id=") {
			return strings.TrimSpace(row)
		}
	}
	return ""
}

func formatSyncId(grnId int64, repeatId string) string {
	if repeatId == "" {
		return fmt.Sprintf("sync-id=%d", grnId)
	} else {
		return fmt.Sprintf("sync-id=%d/%s", grnId, repeatId)
	}
}

func isIgnoreTitle(title string) bool {
	ignores := regexp.MustCompile(`(?mi)[【\[](skip|延期|)[】\]]`)
	return ignores.MatchString(title)
}
