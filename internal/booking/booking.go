// Package booking contains the stay rules shared by availability and booking.
package booking

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid  = errors.New("invalid booking request")
	ErrNotFound = errors.New("resource not found")
	ErrConflict = errors.New("no suitable room available")
)

type Stay struct {
	HotelID  string `json:"hotel_id"`
	CheckIn  string `json:"check_in"`
	CheckOut string `json:"check_out"`
	Guests   int    `json:"guests"`
	RoomType string `json:"room_type,omitempty"`
}

type Request struct {
	Stay
	GuestName string `json:"guest_name"`
}

func (s Stay) Dates() (time.Time, time.Time) {
	in, _ := time.Parse(time.DateOnly, s.CheckIn)
	out, _ := time.Parse(time.DateOnly, s.CheckOut)
	return in, out
}

func (s Stay) Validate(now time.Time) error {
	in, err := time.Parse(time.DateOnly, s.CheckIn)
	if err != nil {
		return ErrInvalid
	}
	out, err := time.Parse(time.DateOnly, s.CheckOut)
	if err != nil {
		return ErrInvalid
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if strings.TrimSpace(s.HotelID) == "" || len(s.HotelID) > 36 || in.Before(today) || !out.After(in) || out.Sub(in) > 30*24*time.Hour || s.Guests < 1 || s.Guests > 4 {
		return ErrInvalid
	}
	if s.RoomType != "" && s.RoomType != "single" && s.RoomType != "double" && s.RoomType != "deluxe" {
		return ErrInvalid
	}
	return nil
}

func (r Request) Validate(now time.Time) error {
	if err := r.Stay.Validate(now); err != nil {
		return err
	}
	if strings.TrimSpace(r.GuestName) == "" || utf8.RuneCountInString(r.GuestName) > 200 {
		return ErrInvalid
	}
	return nil
}

type Room struct {
	ID       string `json:"id"`
	HotelID  string `json:"hotel_id"`
	Number   string `json:"room_number"`
	Type     string `json:"room_type"`
	Capacity int    `json:"capacity"`
}

type Details struct {
	ID         string    `json:"id"`
	Reference  string    `json:"reference"`
	HotelID    string    `json:"hotel_id"`
	HotelName  string    `json:"hotel_name"`
	RoomID     string    `json:"room_id"`
	RoomNumber string    `json:"room_number"`
	RoomType   string    `json:"room_type"`
	Capacity   int       `json:"capacity"`
	GuestName  string    `json:"guest_name"`
	GuestCount int       `json:"guest_count"`
	CheckIn    string    `json:"check_in"`
	CheckOut   string    `json:"check_out"`
	CreatedAt  time.Time `json:"created_at"`
	EventID    string    `json:"event_id"`
	Delivery   string    `json:"event_delivery"`
}
