package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	DB   *sql.DB
	Key  []byte
	Mode string
	// Serialises read-modify-write cycles on cameras so a slow probe cannot
	// overwrite a rename that landed while it was running.
	writeMu sync.Mutex
}

// Either *sql.DB or *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

const (
	upsertCameraSQL     = "INSERT INTO cameras(id,payload,secret) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,secret=excluded.secret"
	upsertConnectionSQL = "INSERT INTO connections(id,payload,secret) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,secret=excluded.secret"
	upsertSettingSQL    = "INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value"
)

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
	// Pragmas in the DSN apply to every pooled connection, not only the first.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS cameras(id TEXT PRIMARY KEY,payload TEXT NOT NULL,secret BLOB NOT NULL);
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

// lookupSetting distinguishes "unset" from a broken database.
func (s *Store) lookupSetting(key string) (string, error) {
	var v string
	err := s.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}
func (s *Store) setting(key string) string {
	v, err := s.lookupSetting(key)
	if err != nil {
		log.Printf("setting %s: %v", key, err)
	}
	return v
}
func upsertSetting(x execer, k, v string) error {
	_, err := x.Exec(upsertSettingSQL, k, v)
	return err
}
func (s *Store) setSetting(k, v string) error { return upsertSetting(s.DB, k, v) }

func (s *Store) decodeCamera(payload string, secret []byte) (Camera, error) {
	var c Camera
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		return c, err
	}
	raw, err := unseal(s.Key, secret)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c.Source)
	return c, err
}
func (s *Store) upsertCamera(x execer, c Camera) error {
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
	_, err = x.Exec(upsertCameraSQL, c.ID, p, sec)
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
		c, err := s.decodeCamera(p, sec)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	applyCameraOrder(list, order)
	return list, rows.Err()
}
func (s *Store) camera(id string) (Camera, error) {
	var p string
	var sec []byte
	if err := s.DB.QueryRow("SELECT payload,secret FROM cameras WHERE id=?", id).Scan(&p, &sec); err != nil {
		return Camera{}, err
	}
	return s.decodeCamera(p, sec)
}
func (s *Store) saveCamera(c Camera) error { return s.upsertCamera(s.DB, c) }

// updateCamera applies only the caller's fields on top of the current row.
// Returns sql.ErrNoRows when the camera does not exist.
func (s *Store) updateCamera(id string, apply func(*Camera)) (Camera, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	c, err := s.camera(id)
	if err != nil {
		return c, err
	}
	apply(&c)
	return c, s.saveCamera(c)
}
func (s *Store) mergeCamera(c Camera) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
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
func (s *Store) decodeConnection(payload string, secret []byte) (Connection, error) {
	var c Connection
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		return c, err
	}
	raw, err := unseal(s.Key, secret)
	if err != nil {
		return c, err
	}
	var creds [2]string
	if err = json.Unmarshal(raw, &creds); err != nil {
		return c, err
	}
	c.Username, c.Password = creds[0], creds[1]
	return c, nil
}
func (s *Store) upsertConnection(x execer, c Connection) error {
	p, err := json.Marshal(c)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal([2]string{c.Username, c.Password})
	sec, err := seal(s.Key, raw)
	if err != nil {
		return err
	}
	_, err = x.Exec(upsertConnectionSQL, c.ID, p, sec)
	return err
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
		c, err := s.decodeConnection(p, sec)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}
func (s *Store) saveConnection(c Connection) error { return s.upsertConnection(s.DB, c) }
func (s *Store) event(kind, msg string) {
	if _, err := s.DB.Exec("INSERT INTO events(created,kind,message) VALUES(?,?,?)", time.Now().Unix(), kind, msg); err != nil {
		log.Printf("event %s: %v", kind, err)
		return
	}
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
