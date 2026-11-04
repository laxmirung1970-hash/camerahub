package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	DB   *sql.DB
	Key  []byte
	Mode string
}

func openStore(dir, mode string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, mode+".key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		err = os.WriteFile(keyPath, key, 0600)
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid storage key")
	}
	path := filepath.Join(dir, mode+".sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS cameras(id TEXT PRIMARY KEY,payload TEXT NOT NULL,secret BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS connections(id TEXT PRIMARY KEY,payload TEXT NOT NULL,secret BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(hash TEXT PRIMARY KEY,name TEXT NOT NULL,kind TEXT NOT NULL,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,created INTEGER NOT NULL,kind TEXT NOT NULL,message TEXT NOT NULL);`); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0600)
	return &Store{DB: db, Key: key, Mode: mode}, nil
}
func seal(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}
func unseal(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("invalid encrypted data")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
}
func (s *Store) setting(key string) string {
	var v string
	_ = s.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&v)
	return v
}
func (s *Store) setSetting(k, v string) error {
	_, err := s.DB.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v)
	return err
}
func (s *Store) cameras() ([]Camera, error) {
	order := s.setting("camera_order")
	rows, err := s.DB.Query("SELECT payload,secret FROM cameras ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Camera{}
	for rows.Next() {
		var p string
		var sec []byte
		if err = rows.Scan(&p, &sec); err != nil {
			return nil, err
		}
		var c Camera
		if err = json.Unmarshal([]byte(p), &c); err != nil {
			return nil, err
		}
		raw, e := unseal(s.Key, sec)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &c.Source); e != nil {
			return nil, e
		}
		list = append(list, c)
	}
	applyCameraOrder(list, order)
	return list, rows.Err()
}
func (s *Store) camera(id string) (Camera, error) {
	var p string
	var sec []byte
	c := Camera{}
	if err := s.DB.QueryRow("SELECT payload,secret FROM cameras WHERE id=?", id).Scan(&p, &sec); err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(p), &c); err != nil {
		return c, err
	}
	raw, err := unseal(s.Key, sec)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c.Source)
	return c, err
}
func (s *Store) saveCamera(c Camera) error {
	p, err := json.Marshal(c)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(c.Source)
	if err != nil {
		return err
	}
	sec, err := seal(s.Key, raw)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO cameras(id,payload,secret) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,secret=excluded.secret", c.ID, p, sec)
	return err
}
func (s *Store) mergeCamera(c Camera) error {
	old, err := s.camera(c.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return s.saveCamera(mergeCameraRecord(c, old, err == nil))
}

func mergeCameraRecord(c, old Camera, exists bool) Camera {
	// Discovery sees an inventory record, not a live video frame. It must not
	// make an unreachable camera's last video contact look recent.
	if c.Status == "pending" {
		if exists {
			c.LastSeen = old.LastSeen
		} else {
			c.LastSeen = time.Time{}
		}
	}
	if exists {
		// Cloud discovery is an inventory refresh, not a new video test. Keep
		// the last result while the same device/source is still advertised.
		if c.Status == "pending" && (old.Status == "online" || old.Status == "error") &&
			c.Host == old.Host && c.Model == old.Model && c.Source.Low == old.Source.Low && c.Source.High == old.Source.High {
			c.Status = old.Status
			c.Message = old.Message
		}
		c.Name = old.Name
		c.Group = old.Group
		c.Favorite = old.Favorite
		c.Verification = old.Verification
		c.AudioVerified = old.AudioVerified
		c.PTZVerified = old.PTZVerified
		c.PlaybackMode = old.PlaybackMode
		if c.Vendor == "xiaomi" && old.Source.PTZProtocol != "" {
			c.Source.PTZProtocol = old.Source.PTZProtocol
			c.PTZ = old.PTZ
		}
	}
	return c
}
func (s *Store) connections() ([]Connection, error) {
	rows, err := s.DB.Query("SELECT payload,secret FROM connections ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Connection{}
	for rows.Next() {
		var p string
		var sec []byte
		if err = rows.Scan(&p, &sec); err != nil {
			return nil, err
		}
		var c Connection
		if err = json.Unmarshal([]byte(p), &c); err != nil {
			return nil, err
		}
		raw, e := unseal(s.Key, sec)
		if e != nil {
			return nil, e
		}
		var creds [2]string
		if e = json.Unmarshal(raw, &creds); e != nil {
			return nil, e
		}
		c.Username = creds[0]
		c.Password = creds[1]
		list = append(list, c)
	}
	return list, rows.Err()
}
func (s *Store) saveConnection(c Connection) error {
	p, err := json.Marshal(c)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal([2]string{c.Username, c.Password})
	sec, err := seal(s.Key, raw)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO connections(id,payload,secret) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,secret=excluded.secret", c.ID, p, sec)
	return err
}
func (s *Store) event(kind, msg string) {
	_, _ = s.DB.Exec("INSERT INTO events(created,kind,message) VALUES(?,?,?)", time.Now().Unix(), kind, msg)
	_, _ = s.DB.Exec("DELETE FROM events WHERE id NOT IN (SELECT id FROM events ORDER BY id DESC LIMIT 200)")
}
func (s *Store) events() []Event {
	rows, err := s.DB.Query("SELECT id,created,kind,message FROM events ORDER BY id DESC LIMIT 30")
	if err != nil {
		return []Event{}
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var ts int64
		if rows.Scan(&e.ID, &ts, &e.Kind, &e.Message) == nil {
			e.Time = time.Unix(ts, 0)
			out = append(out, e)
		}
	}
	return out
}
