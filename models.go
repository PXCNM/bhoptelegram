package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// DATABASE DATA
type Server struct {
	ID   int64
	Name string
}

type BhopMap struct {
	ID             int64
	Name           string
	Tier           *int
	Time           *float64
	Runner         *string
	SourceJumpID   *int
	TASTime        *float64
	RunnerTAS      *string
	ServerID       *int64
	TasServerID    *int64
	YouTubeLink    *string
	YouTubeTASLink *string
	FastDLHash     *string
}

type MomentumMode struct { // TODO: Rework the structure to work with momentum modes
	ID   int64
	Name string
}

type MomentumMap struct {
	ID       int64
	Name     string
	Tier     int
	ModeID   int64
	IsRanked bool
}

type MomentumTime struct {
	ID     int64
	MapID  int64
	Runner string
	time   float64
}

// SERVER RESPONSES

// www.sourcejump.net/ajax/records/wrs
type RecordListEntry struct {
	ID      int
	Name    string
	Country string
	Map     string
	Time    string
	WrDif   string
	SteamID string
	Tier    int
}

// www.sourcejump.net/ajax/records/map/{MAP}
type RecordMapEntry struct {
	ID          int
	Name        string
	Country     string
	Map         string
	Hostname    string
	Time        string
	TimeSeconds float64
	WRDif       string
	SteamID     string
	Tier        int
	Date        string
	Video       *string
	Points      int
}

// www.sourcejump.net/ajax/records/id/{ID}
type RecordDetail struct {
	ID       int
	Name     string
	Country  string
	Avatar   *string
	Banned   *int
	Map      string
	Time     string
	SteamID  string
	Tier     int
	Sync     float64
	Strafes  int
	Jumps    int
	Date     string
	IP       string
	Hostname string
	Invalid  any
	BadZones any
	Points   int
}

// www.sourcejump.net/ajax/servers
type ServerListEntry struct {
	ID        int64
	Country   string
	Hostname  string
	IP        string
	Whitelist int
}

func (s ServerListEntry) toDBModel() Server {
	return Server{
		ID:   s.ID,
		Name: s.Hostname,
	}
}

func FormatSeconds(totalSeconds float64) string {
	hours := int(totalSeconds / 3600)
	remainder := math.Mod(totalSeconds, 3600)
	minutes := int(remainder / 60)
	seconds := math.Mod(remainder, 60)

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%06.3f", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d:%06.3f", minutes, seconds)
	}
	return fmt.Sprintf("%.3f", seconds)
}

func ParseTime(t string) float64 {
	if strings.Contains(t, ":") { // > 59.999
		parts := strings.Split(t, ":")

		// Working with times up to 23:59:59.999
		if len(parts) == 2 {
			seconds, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				return -1.0
			}

			minutes, err := strconv.ParseFloat(parts[0], 64)
			if err != nil {
				return -1.0
			}

			return minutes*60 + seconds
		} else if len(parts) == 3 {
			seconds, err := strconv.ParseFloat(parts[2], 64)
			if err != nil {
				return -1.0
			}

			minutes, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				return -1.0
			}

			hours, err := strconv.ParseFloat(parts[0], 64)
			if err != nil {
				return -1.0
			}

			return hours*3600 + minutes*60 + seconds
		} else {
			return -1.0
		}
	} else {
		val, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return -1.0
		}

		return val
	}
}
