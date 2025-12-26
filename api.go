package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	URLRecentWRs = "https://www.sourcejump.net/ajax/records/wrs"
	URLFastDL    = "https://main.fastdl.me/69.html"

	FURLRecordsID  = "https://www.sourcejump.net/ajax/records/id/%d"
	FURLRecordsMap = "https://www.sourcejump.net/ajax/records/map/%s"
	FURLSheet      = "https://docs.google.com/spreadsheets/d/%s/export?format=csv&gid=%s"

	SheetID  = "1D02pV-VWrJK8M_GVpk434YvfEZbkfUIplEQlOlq0rTc"
	SheetGID = "1663410541"
)

func FetchRecentRecords() ([]RecordListEntry, error) {
	resp, err := http.Get(URLRecentWRs)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var recentRecords []RecordListEntry
	if err := json.NewDecoder(resp.Body).Decode(&recentRecords); err != nil {
		return nil, err
	}

	return recentRecords, nil
}

func SyncTASData(storage *Storage) error {
	url := fmt.Sprintf(FURLSheet, SheetID, SheetGID)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch TAS sheet: %w", err)
	}
	defer resp.Body.Close()

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to parse CSV: %w", err)
	}

	for i, row := range records {
		if i == 0 {
			continue
		}

		if len(row) < 2 { // At least map and time
			continue
		}

		mapName := strings.TrimSpace(row[0])
		timeStr := strings.TrimSpace(row[1])
		runner := strings.TrimSpace(row[2])
		server := strings.TrimSpace(row[3])

		tasTime := ParseTime(timeStr)

		if tasTime < 0 {
			continue
		}

		if err := storage.UpdateTASRecord(mapName, tasTime, runner, server); err != nil {
			log.Println("Couldn't update tas record: ", err)
		}
	}

	return nil
}

func FetchMapWR(mapName string) (*RecordMapEntry, error) {
	safeMapName := url.PathEscape(mapName)
	url := fmt.Sprintf(FURLRecordsMap, safeMapName)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var records []RecordMapEntry
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, err
	}
	return &records[0], nil
}

func FetchRecordDetail(recordID int) (*RecordDetail, error) {
	resp, err := http.Get(fmt.Sprintf(FURLRecordsID, recordID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var detail RecordDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func GetFastDLHash(mapName string) (string, error) {
	resp, err := http.Get(URLFastDL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	htmlContent := string(bodyBytes)

	// Taking out map hash using regex
	// We use QuoteMeta to safely escape special characters in the map name.
	pattern := fmt.Sprintf(`<td><a\s+href="[^"]*">%s</a></td>\s*<td>([a-f0-9]+)</td>`, regexp.QuoteMeta(mapName))

	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(htmlContent)

	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", nil
}

func ProcessAndSyncRecords(store *Storage) error {
	records, err := FetchRecentRecords()
	if err != nil {
		return fmt.Errorf("failed to fetch records list: %w", err)
	}

	for _, rec := range records {
		mapID, sourcejumpID, err := store.GetMapState(rec.Map)
		if err != nil {
			log.Printf("DB Error checking %s: %v", rec.Map, err)
			continue
		}

		// We already have the newest record saved
		if mapID > 0 && sourcejumpID == rec.ID {
			continue
		}

		detail, err := FetchRecordDetail(rec.ID)
		if err != nil {
			log.Printf("Failed to fetch detail for %d: %v", rec.ID, err)
			continue
		}

		realMapID, _, err := store.GetMapState(detail.Map)
		if err != nil {
			continue
		}

		if realMapID != -1 {
			if err := store.UpdateMapRecord(realMapID, *detail); err != nil {
				log.Printf("Failed to update map %s: %v", detail.Map, err)
			}
		} else {
			hash, _ := GetFastDLHash(detail.Map)
			if err := store.CreateMap(*detail, hash); err != nil {
				log.Printf("Failed to create map %s: %v", detail.Map, err)
			}
		}
	}
	return nil
}

func FetchAllFastDLMaps() (map[string]string, error) {
	resp, err := http.Get(URLFastDL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`<td><a\s+href="#">([^<]+)</a></td>\s*<td>([a-f0-9]+)</td>`)
	matches := re.FindAllStringSubmatch(string(bodyBytes), -1)

	result := make(map[string]string)
	for _, match := range matches {
		if len(match) == 3 {
			// match[1] is Map Name, match[2] is Hash
			result[match[1]] = match[2]
		}
	}
	return result, nil
}

func BulkSyncFastDL(store *Storage) error {
	fastDLMaps, err := FetchAllFastDLMaps()
	if err != nil {
		return err
	}

	tx, err := store.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO bhop_map (name, fastdl_hash) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET fastdl_hash=excluded.fastdl_hash;
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for name, hash := range fastDLMaps {
		_, err := stmt.Exec(name, hash)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}
