package main

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/joho/godotenv"
	"github.com/otoyo/garoon"
	"github.com/urfave/cli/v2"
	"google.golang.org/api/calendar/v3"
)

func main() {
	godotenv.Load()

	app := cli.NewApp()
	app.Name = "grn-gcal-sync"
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:     "grn-user",
			Usage:    "garoon login user name.",
			EnvVars:  []string{"GAROON_USER"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "grn-user-id",
			Usage:   "garoon target user id",
			EnvVars: []string{"GAROON_USER_ID"},
		},
		&cli.StringFlag{
			Name:     "grn-pass",
			Usage:    "garoon login password",
			EnvVars:  []string{"GAROON_PASS"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "grn-url",
			Usage:   "garoon package version login url",
			EnvVars: []string{"GAROON_URL"},
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
				grnUrl := c.String("grn-url")
				var client *garoon.Client
				var err error
				if grnUrl != "" {
					client, err = garoon.NewClientWithBaseUrl(
						grnUrl,
						c.String("grn-user"),
						c.String("grn-pass"),
					)
					if err != nil {
						panic(err)
					}
				} else {
					client, err = garoon.NewClient(
						c.String("grn-subdomain"),
						c.String("grn-user"),
						c.String("grn-pass"),
					)
				}

				grn := NewGrnClient(client)
				gcal, err := NewGcalClient(!c.Bool("no-interactive"), c.String("gcal-token-path"))
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
						if findSyncId(dstEvent.Description) == formatSyncId(srcEvent.ID) {
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
					syncId := formatSyncId(srcEvent.ID)
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
						diff := cmp.Diff(exceptEvent, foundGcalEvent, cmpopts.IgnoreFields(*exceptEvent, "Created", "Creator", "Etag", "ICalUID", "Id", "HtmlLink", "Status", "Updated", "Reminders", "Organizer", "Kind", "Sequence"))
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
									waitTime := (2 << retries) + rand.Intn(1000)/1000
									time.Sleep(time.Duration(waitTime) * time.Second)

									retries++
									continue
								}
								break
							}
							if lasterr != nil {
								panic(err)
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

func formatSyncId(grnId int64) string {
	return fmt.Sprintf("sync-id=%d", grnId)
}

func isIgnoreTitle(title string) bool {
	ignores := regexp.MustCompile(`(?mi)[【\[](skip|延期|)[】\]]`)
	return ignores.MatchString(title)
}
