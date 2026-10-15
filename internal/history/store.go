package history

import (
	"encoding/json"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Sample is one persisted telemetry point (already scaled to real units).
type Sample struct {
	TS           int64   `json:"ts"`
	SOC          float64 `json:"soc"`
	BatteryPower float64 `json:"battery_power"`
	BatteryV     float64 `json:"battery_v"`
	GridPower    float64 `json:"grid_power"`
	LoadPower    float64 `json:"load_power"`
	InvPower     float64 `json:"inv_power"`
	SolarPower   float64 `json:"solar_power"`
	PVToLoad     float64 `json:"pv_to_load"`
	PVToBattery  float64 `json:"pv_to_battery"`
	PVToGrid     float64 `json:"pv_to_grid"`
	BatToLoad    float64 `json:"battery_to_load"`
	GridToLoad   float64 `json:"grid_to_load"`
	BatToGrid    float64 `json:"battery_to_grid"`
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS samples (
  ts INTEGER PRIMARY KEY,
  soc REAL,
  battery_power REAL,
  battery_v REAL,
  grid_power REAL,
  load_power REAL,
  inv_power REAL,
  solar_power REAL,
  pv_to_load REAL,
  pv_to_battery REAL,
  pv_to_grid REAL,
  battery_to_load REAL,
  grid_to_load REAL,
  battery_to_grid REAL
);
CREATE INDEX IF NOT EXISTS idx_samples_ts ON samples(ts);
`)
	return err
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Insert(x Sample) error {
	if x.TS == 0 {
		x.TS = time.Now().Unix()
	}
	_, err := s.db.Exec(`
INSERT OR REPLACE INTO samples (
  ts, soc, battery_power, battery_v, grid_power, load_power, inv_power, solar_power,
  pv_to_load, pv_to_battery, pv_to_grid, battery_to_load, grid_to_load, battery_to_grid
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.TS, x.SOC, x.BatteryPower, x.BatteryV, x.GridPower, x.LoadPower, x.InvPower, x.SolarPower,
		x.PVToLoad, x.PVToBattery, x.PVToGrid, x.BatToLoad, x.GridToLoad, x.BatToGrid,
	)
	return err
}

// Query returns samples since start (unix), at most limit rows, newest last.
func (s *Store) Query(start, end int64, limit int) ([]Sample, error) {
	if limit <= 0 || limit > 20000 {
		limit = 2000
	}
	q := `SELECT ts,soc,battery_power,battery_v,grid_power,load_power,inv_power,solar_power,
	  pv_to_load,pv_to_battery,pv_to_grid,battery_to_load,grid_to_load,battery_to_grid
	  FROM samples WHERE ts >= ?`
	args := []any{start}
	if end > 0 {
		q += " AND ts <= ?"
		args = append(args, end)
	}
	q += " ORDER BY ts ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Sample, 0, 256)
	for rows.Next() {
		var x Sample
		if err := rows.Scan(
			&x.TS, &x.SOC, &x.BatteryPower, &x.BatteryV, &x.GridPower, &x.LoadPower,
			&x.InvPower, &x.SolarPower, &x.PVToLoad, &x.PVToBattery, &x.PVToGrid,
			&x.BatToLoad, &x.GridToLoad, &x.BatToGrid,
		); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Count returns number of stored samples.
func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM samples`).Scan(&n)
	return n, err
}

// Prune deletes samples older than olderThan unix seconds.
func (s *Store) Prune(olderThan int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM samples WHERE ts < ?`, olderThan)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) String() string {
	n, _ := s.Count()
	return fmt.Sprintf("history samples=%d", n)
}

// FromSnapshot extracts scaled values from a JSON-decoded snapshot map.
func FromSnapshot(ts int64, m map[string]any) Sample {
	fnum := func(v any) float64 {
		switch x := v.(type) {
		case float64:
			return x
		case float32:
			return float64(x)
		case int:
			return float64(x)
		case int32:
			return float64(x)
		case int64:
			return float64(x)
		case json.Number:
			f, _ := x.Float64()
			return f
		}
		return 0
	}
	get := func(path ...string) float64 {
		var cur any = m
		for _, k := range path {
			switch node := cur.(type) {
			case map[string]any:
				cur = node[k]
			case []any:
				idx := 0
				fmt.Sscanf(k, "%d", &idx)
				if idx < 0 || idx >= len(node) {
					return 0
				}
				cur = node[idx]
			default:
				return 0
			}
		}
		return fnum(cur)
	}
	phase := func(root, key string) float64 {
		g, _ := m[root].(map[string]any)
		if g == nil {
			return 0
		}
		ph, _ := g["phases"].([]any)
		if len(ph) == 0 {
			return 0
		}
		p, _ := ph[0].(map[string]any)
		if p == nil {
			return 0
		}
		return fnum(p[key])
	}
	solar := get("pv_panels", "0", "power") + get("pv_panels", "1", "power")
	if solar == 0 {
		solar = get("power_flow", "pv_to_load") + get("power_flow", "pv_to_battery") + get("power_flow", "pv_to_grid")
	}
	return Sample{
		TS:           ts,
		SOC:          get("battery", "state_of_charge"),
		BatteryPower: get("battery", "power"),
		BatteryV:     get("battery", "voltage"),
		GridPower:    phase("grid", "active_power"),
		LoadPower:    phase("load", "active_power"),
		InvPower:     phase("inverter", "active_power"),
		SolarPower:   solar,
		PVToLoad:     get("power_flow", "pv_to_load"),
		PVToBattery:  get("power_flow", "pv_to_battery"),
		PVToGrid:     get("power_flow", "pv_to_grid"),
		BatToLoad:    get("power_flow", "battery_to_load"),
		GridToLoad:   get("power_flow", "grid_to_load"),
		BatToGrid:    get("power_flow", "battery_to_grid"),
	}
}
