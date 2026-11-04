package main

import "time"

type Camera struct {
	ID            string    `json:"id"`
	Vendor        string    `json:"vendor"`
	Ref           string    `json:"ref"`
	Name          string    `json:"name"`
	Group         string    `json:"group"`
	Model         string    `json:"model"`
	ParentID      string    `json:"parentId,omitempty"`
	Lens          int       `json:"lens,omitempty"`
	LensCount     int       `json:"lensCount,omitempty"`
	Host          string    `json:"host"`
	Region        string    `json:"region,omitempty"`
	Favorite      bool      `json:"favorite"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
	Audio         bool      `json:"audio"`
	PTZ           bool      `json:"ptz"`
	Verification  string    `json:"verification"`
	AudioVerified bool      `json:"audioVerified"`
	PTZVerified   bool      `json:"ptzVerified"`
	PlaybackMode  string    `json:"playbackMode"`
	LastSeen      time.Time `json:"lastSeen"`
	Source        Source    `json:"-"`
}
type Source struct {
	Low         string `json:"low"`
	High        string `json:"high"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	DeviceURL   string `json:"deviceUrl,omitempty"`
	MediaURL    string `json:"mediaUrl,omitempty"`
	PTZURL      string `json:"ptzUrl,omitempty"`
	Profile     string `json:"profile,omitempty"`
	PTZProtocol string `json:"ptzProtocol,omitempty"`
	Connection  string `json:"connection,omitempty"`
}
type Connection struct {
	ID       string    `json:"id"`
	Vendor   string    `json:"vendor"`
	Name     string    `json:"name"`
	Account  string    `json:"account,omitempty"`
	Region   string    `json:"region,omitempty"`
	Enabled  bool      `json:"enabled"`
	LastSync time.Time `json:"lastSync"`
	Status   string    `json:"status"`
	Message  string    `json:"message,omitempty"`
	Username string    `json:"-"`
	Password string    `json:"-"`
}
type Event struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
}
type Principal struct {
	ID   string
	Kind string
	Name string
}
