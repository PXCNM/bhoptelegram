package main

import (
	"database/sql"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type Storage struct {
	db *sql.DB
}

func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	storage := &Storage{db: db}

	if err := storage.InitDB(); err != nil {
		db.Close()
		return nil, err
	}

	return storage, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) InitDB() error {
	if _, err := s.db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return err
	}

	queries := []string{
		`CREATE TABLE IF NOT EXISTS server (
				id INTEGER PRIMARY KEY,
				name TEXT NOT NULL UNIQUE
			);`,

		`CREATE TABLE IF NOT EXISTS bhop_map (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL UNIQUE,
				tier INTEGER,
				time REAL,
				runner TEXT,
				sourcejump_id INTEGER
				tas_time REAL,
				runner_tas TEXT,
				server_id INTEGER,
				tas_server_id INTEGER,
				youtube_link TEXT,
				youtube_tas_link TEXT,
				fastdl_hash TEXT,
				FOREIGN KEY(server_id) REFERENCES server(id),
				FOREIGN KEY(tas_server_id) REFERENCES server(id)
			);`,
	}

	for _, query := range queries {
		if _, err := s.db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}

// MAP RELATED
func (s *Storage) GetFullMapDetails(mapName string) (*BhopMap, string, string, error) {
	query := `
		SELECT
			m.id, m.name, m.tier, m.time, m.runner,
			m.tas_time, m.runner_tas, m.fastdl_hash,
			s1.name, s2.name
		FROM bhop_map m
		LEFT JOIN server s1 ON m.server_id = s1.id
		LEFT JOIN server s2 ON m.tas_server_id = s2.id
		WHERE m.name = ?
	`

	var m BhopMap
	var wrServerName, tasServerName sql.NullString

	err := s.db.QueryRow(query, mapName).Scan(
		&m.ID, &m.Name, &m.Tier, &m.Time, &m.Runner,
		&m.TASTime, &m.RunnerTAS, &m.FastDLHash,
		&wrServerName, &tasServerName,
	)

	if err == sql.ErrNoRows {
		return nil, "", "", nil
	}
	if err != nil {
		return nil, "", "", err
	}

	return &m, wrServerName.String, tasServerName.String, nil
}

func (s *Storage) UpdateMapFromBackfill(r RecordMapEntry) error {
	serverID, err := s.GetOrCreateServer(r.Hostname)
	if err != nil {
		return err
	}

	var serverIDArg any = serverID
	if serverID == 0 {
		serverIDArg = nil
	}

	query := `
			UPDATE bhop_map
			SET time = ?, runner = ?, tier = ?, server_id = ?, sourcejump_id = ?
			WHERE name = ?
	`
	_, err = s.db.Exec(query, r.TimeSeconds, r.Name, r.Tier, serverIDArg, r.ID, r.Map)
	return err
}

func (s *Storage) CreateMap(r RecordDetail, fastDLHash string) error {
	timeVal := ParseTime(r.Time)
	serverID, err := s.GetOrCreateServer(r.Hostname)
	if err != nil {
		return err
	}

	var serverIDArg any = serverID
	if serverID == 0 {
		serverIDArg = nil
	}

	query := `
		INSERT INTO bhop_map (name, tier, time, runner, server_id, fastdl_hash)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err = s.db.Exec(query, r.Map, r.Tier, timeVal, r.Name, serverIDArg, fastDLHash)
	return err
}

func (s *Storage) GetMapState(mapName string) (int64, int, error) {
	var id int64
	var recordID sql.NullInt64

	err := s.db.QueryRow("SELECT id, sourcejump_id FROM bhop_map WHERE name = ?", mapName).Scan(&id, &recordID)

	if err == sql.ErrNoRows {
		return -1, 0, nil
	}
	if err != nil {
		return -1, 0, err
	}

	if recordID.Valid {
		return id, int(recordID.Int64), nil
	}
	return id, 0, nil
}

func (s *Storage) SearchMaps(query string) ([]string, error) {
	searchTerm := "%" + strings.TrimSpace(query) + "%"
	rows, err := s.db.Query("SELECT name FROM bhop_map WHERE name LIKE ? LIMIT 50", searchTerm)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var maps []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		maps = append(maps, name)
	}
	return maps, nil
}

// RECORD RELATED
func (s *Storage) UpdateTASRecord(mapName string, time float64, runner string, serverName string) error {
	serverID, err := s.GetOrCreateServer(serverName)
	if err != nil {
		return err
	}

	var serverIDArg any = serverID
	if serverID == 0 {
		serverIDArg = nil
	}

	query := `UPDATE bhop_map SET tas_time = ?, runner_tas = ?, tas_server_id = ? WHERE name = ?`
	_, err = s.db.Exec(query, time, runner, serverIDArg, mapName)
	return err
}

func (s *Storage) UpdateMapRecord(mapID int64, r RecordDetail) error {
	timeVal := ParseTime(r.Time)
	serverID, err := s.GetOrCreateServer(r.Hostname)
	if err != nil {
		return err
	}

	var serverIDArg any = serverID
	if serverID == 0 {
		serverIDArg = nil
	}

	// Added latest_record_id = ?
	query := `UPDATE bhop_map SET time = ?, runner = ?, server_id = ?, sourcejump_id = ? WHERE id = ?`
	_, err = s.db.Exec(query, timeVal, r.Name, serverIDArg, r.ID, mapID)
	return err
}

// SERVER RELATED
func (s *Storage) GetOrCreateServer(name string) (int64, error) {
	if name == "" {
		return 0, nil
	}

	var id int64
	err := s.db.QueryRow("SELECT id FROM server WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}

	_, err = s.db.Exec("INSERT OR IGNORE INTO server (name) VALUES (?)", name)
	if err != nil {
		return 0, err
	}

	err = s.db.QueryRow("SELECT id FROM server WHERE name = ?", name).Scan(&id)
	return id, err
}
