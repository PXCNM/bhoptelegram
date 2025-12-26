package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	tele "gopkg.in/telebot.v3"
)

/* TODOS:
 * 		Add support for momentum
 * 		Find youtube videos from selected channels, parse some of tas videos from excel table
 * 		Notiffications on: new wr (all/map), new momentum map (all/mode), momentum map status changed (all/map/mode), new video (all)
 */

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, relying on OS environment variables")
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("Error: TELEGRAM_BOT_TOKEN is not set")
	}

	storage, err := NewStorage("./bot_data.db")
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer storage.Close()

	log.Println("Running initial data sync...")
	if err := BulkSyncFastDL(storage); err != nil {
		log.Println("Error syncing FastDL:", err)
	}

	if err := SyncTASData(storage); err != nil {
		log.Println("Error syncing TAS:", err)
	}

	pref := tele.Settings{
		Token:  botToken,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	go startBackgroundTasks(storage)

	b.Handle("/start", func(c tele.Context) error {
		return c.Send("Hello! This a bhop record checker + movement map downloader. Type a map name to search.\nType /info for a github link")
	})

	b.Handle("/info", func(c tele.Context) error {
		return c.Send("Bot Source Code: https://github.com/PXCNM/bhoptelegram")
	})

	b.Handle(tele.OnCallback, func(c tele.Context) error {
		mapName := c.Callback().Data

		m, wrServer, tasServer, err := storage.GetFullMapDetails(strings.TrimSpace(mapName))
		if err != nil {
			log.Println("Error during fetching map details: ", err)
			return c.Send("Error fetching map details.")
		}
		if m == nil {
			return c.Send("Map not found.")
		}

		m = ensureMapData(storage, m)

		// Doing the same thing just to get server name... For new servers it did not show up
		if m.Time != nil && wrServer == "" {
			_, wrServer, _, _ = storage.GetFullMapDetails(m.Name)
		}

		msg := formatMapMessage(m, wrServer, tasServer)
		c.Respond()

		return c.Edit(msg, tele.ModeHTML)
	})

	b.Handle(tele.OnText, func(c tele.Context) error {
		query := c.Text()

		results, err := storage.SearchMaps(query, 50)
		if err != nil {
			log.Println("Search error:", err)
			return c.Send("An error occurred while searching.")
		}

		if len(results) == 0 {
			return c.Send("No maps found.")
		}

		if len(results) == 1 {
			m, wrServer, tasServer, err := storage.GetFullMapDetails(strings.TrimSpace(results[0]))
			if err != nil {
				return c.Send("Error fetching details.")
			}

			m = ensureMapData(storage, m)

			// The same thing as above...
			if m.Time != nil && wrServer == "" {
				_, wrServer, _, _ = storage.GetFullMapDetails(m.Name)
			}

			return c.Send(formatMapMessage(m, wrServer, tasServer), tele.ModeHTML)
		}

		menu := &tele.ReplyMarkup{}
		var buttons []tele.Btn

		for _, mapName := range results {
			buttons = append(buttons, menu.Data(mapName, mapName))
		}

		rows := menu.Split(2, buttons)

		menu.Inline(rows...)

		return c.Send("Multiple maps found. Please select one:", menu)
	})

	log.Println("Bot is working...")
	b.Start()
}

func startBackgroundTasks(store *Storage) {
	tickerSJ := time.NewTicker(30 * time.Minute)
	tickerTASFastDL := time.NewTicker(6 * time.Hour)

	for {
		select {
		case <-tickerSJ.C:
			log.Println("Syncing Recent Records...")
			if err := ProcessAndSyncRecords(store); err != nil {
				log.Println("Error syncing records:", err)
			}
		case <-tickerTASFastDL.C:
			log.Println("Syncing FastDL & TAS...")
			if err := BulkSyncFastDL(store); err != nil {
				log.Println("Error syncing FastDL:", err)
			}
			if err := SyncTASData(store); err != nil {
				log.Println("Error syncing TAS:", err)
			}
		}
	}
}

func ensureMapData(store *Storage, m *BhopMap) *BhopMap {
	if m.Time != nil {
		return m
	}

	log.Printf("Lazy loading WR for map: %s", m.Name)

	wr, err := FetchMapWR(m.Name)
	if err != nil {
		log.Printf("Failed to lazy load WR for %s: %v", m.Name, err)
		return m
	}

	if err := store.SaveMapWR(wr); err != nil {
		log.Printf("Failed to save WR for %s: %v", m.Name, err)
	}

	val := wr.TimeSeconds
	m.Time = &val
	runner := wr.Name
	m.Runner = &runner

	return m
}

func formatMapMessage(m *BhopMap, wrServer string, tasServer string) string {
	wrLine := "No WR recorded"
	if m.Time != nil && m.Runner != nil {
		formattedTime := FormatSeconds(*m.Time)

		if wrServer != "" {
			wrLine = fmt.Sprintf("<b>%s</b> by %s (<code>%s</code>)", formattedTime, *m.Runner, wrServer)
		} else {
			wrLine = fmt.Sprintf("<b>%s</b> by %s", formattedTime, *m.Runner)
		}
	}

	tasLine := "No TAS recorded"
	if m.TASTime != nil && m.RunnerTAS != nil {
		formattedTas := FormatSeconds(*m.TASTime)
		if tasServer != "" {
			tasLine = fmt.Sprintf("<b>%s</b> by %s (<code>%s</code>)", formattedTas, *m.RunnerTAS, tasServer)
		} else {
			tasLine = fmt.Sprintf("<b>%s</b> by %s", formattedTas, *m.RunnerTAS)
		}
	}

	dlLink := "No FastDL Link"
	if m.FastDLHash != nil && *m.FastDLHash != "" {
		url := fmt.Sprintf("https://main.fastdl.me/h2/%s/%s.bsp.bz2", *m.FastDLHash, m.Name)
		dlLink = fmt.Sprintf("<a href=\"%s\">fastDL</a>", url)
	}

	return fmt.Sprintf(
		"<code>%s</code> (%s)\n"+
			"WR: %s\n"+
			"TAS: %s",
		m.Name, dlLink, wrLine, tasLine,
	)
}
