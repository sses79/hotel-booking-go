package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sijms/go-ora/v2/network"
	"github.com/sses79/hotel-booking-go/internal/booking"
	"github.com/sses79/hotel-booking-go/internal/events"
)

func (s *Store) Available(ctx context.Context, stay booking.Stay) ([]booking.Room, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM Hotels WHERE HotelId = :1", stay.HotelID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, booking.ErrNotFound
	}
	in, out := stay.Dates()
	rows, err := s.db.QueryContext(ctx, `SELECT R.RoomId, R.HotelId, R.RoomNumber, R.RoomType, R.Capacity FROM Rooms R
	WHERE R.HotelId = :1 AND R.Capacity >= :2 AND (:3 IS NULL OR R.RoomType = :4)
	AND NOT EXISTS (SELECT 1 FROM OccupiedNights N WHERE N.RoomId = R.RoomId AND N.StayDate >= :5 AND N.StayDate < :6)
	ORDER BY R.Capacity, R.RoomType, R.RoomNumber`, stay.HotelID, stay.Guests, stay.RoomType, stay.RoomType, in, out)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rooms := make([]booking.Room, 0)
	for rows.Next() {
		var r booking.Room
		if err := rows.Scan(&r.ID, &r.HotelID, &r.Number, &r.Type, &r.Capacity); err != nil {
			return nil, err
		}
		rooms = append(rooms, r)
	}
	return rooms, rows.Err()
}

func uniqueViolation(err error) bool {
	var ora *network.OracleError
	return errors.As(err, &ora) && ora.ErrCode == 1
}

// OccupiedNights has only its primary key as a unique constraint. Consult its
// database-generated name so unrelated uniqueness failures are never retried.
func (s *Store) occupiedKey(ctx context.Context) (string, error) {
	var key string
	err := s.db.QueryRowContext(ctx, "SELECT CONSTRAINT_NAME FROM USER_CONSTRAINTS WHERE TABLE_NAME = 'OCCUPIEDNIGHTS' AND CONSTRAINT_TYPE = 'P'").Scan(&key)
	return key, err
}

func (s *Store) Book(ctx context.Context, req booking.Request) (booking.Details, events.Created, error) {
	rooms, err := s.Available(ctx, req.Stay)
	if err != nil {
		return booking.Details{}, events.Created{}, err
	}
	key, err := s.occupiedKey(ctx)
	if err != nil {
		return booking.Details{}, events.Created{}, err
	}
	for _, room := range rooms {
		d, e, err := s.bookCandidate(ctx, req, room)
		if err == nil {
			return d, e, nil
		}
		if uniqueViolation(err) && strings.Contains(strings.ToUpper(err.Error()), "."+key+")") {
			continue
		}
		return booking.Details{}, events.Created{}, err
	}
	return booking.Details{}, events.Created{}, booking.ErrConflict
}

func (s *Store) bookCandidate(ctx context.Context, req booking.Request, room booking.Room) (booking.Details, events.Created, error) {
	d := booking.Details{ID: uuid.NewString(), Reference: "HB-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")), HotelID: req.HotelID,
		RoomID: room.ID, RoomNumber: room.Number, RoomType: room.Type, Capacity: room.Capacity, GuestName: strings.TrimSpace(req.GuestName), GuestCount: req.Guests,
		CheckIn: req.CheckIn, CheckOut: req.CheckOut, CreatedAt: time.Now().UTC(), EventID: uuid.NewString(), Delivery: "pending"}
	e := events.Created{Type: events.CreatedType, EventID: d.EventID, BookingID: d.ID, Reference: d.Reference, HotelID: d.HotelID, RoomID: d.RoomID, CreatedAt: d.CreatedAt}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return d, e, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT Name FROM Hotels WHERE HotelId = :1", req.HotelID).Scan(&d.HotelName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = booking.ErrNotFound
		}
		return d, e, err
	}
	in, out := req.Dates()
	if _, err := tx.ExecContext(ctx, `INSERT INTO Bookings (BookingId, Reference, HotelId, RoomId, GuestName, GuestCount, CheckInDate, CheckOutDate, CreatedAt)
	VALUES (:1,:2,:3,:4,:5,:6,:7,:8,:9)`, d.ID, d.Reference, d.HotelID, d.RoomID, d.GuestName, d.GuestCount, in, out, d.CreatedAt); err != nil {
		return d, e, err
	}
	for night := in; night.Before(out); night = night.AddDate(0, 0, 1) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO OccupiedNights (RoomId, StayDate, BookingId) VALUES (:1,:2,:3)", room.ID, night, d.ID); err != nil {
			return d, e, err
		}
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return d, e, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO OutboxEvents (EventId, BookingId, Payload, CreatedAt) VALUES (:1,:2,:3,:4)", e.EventID, d.ID, string(payload), d.CreatedAt); err != nil {
		return d, e, err
	}
	// Never retry an uncertain commit or publish unless commit succeeds.
	return d, e, tx.Commit()
}

func (s *Store) Lookup(ctx context.Context, reference string) (booking.Details, error) {
	var d booking.Details
	var in, out time.Time
	var published sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT B.BookingId, B.Reference, B.HotelId, H.Name, B.RoomId, R.RoomNumber, R.RoomType, R.Capacity,
	B.GuestName, B.GuestCount, B.CheckInDate, B.CheckOutDate, B.CreatedAt, E.EventId, E.PublishedAt
	FROM Bookings B JOIN Hotels H ON H.HotelId = B.HotelId JOIN Rooms R ON R.RoomId = B.RoomId
	JOIN OutboxEvents E ON E.BookingId = B.BookingId WHERE B.Reference = :1`, reference).Scan(
		&d.ID, &d.Reference, &d.HotelID, &d.HotelName, &d.RoomID, &d.RoomNumber, &d.RoomType, &d.Capacity,
		&d.GuestName, &d.GuestCount, &in, &out, &d.CreatedAt, &d.EventID, &published)
	if errors.Is(err, sql.ErrNoRows) {
		return d, booking.ErrNotFound
	}
	d.CheckIn, d.CheckOut, d.CreatedAt = in.Format(time.DateOnly), out.Format(time.DateOnly), d.CreatedAt.UTC()
	d.Delivery = "pending"
	if published.Valid {
		d.Delivery = "published"
	}
	return d, err
}

func (s *Store) Pending(ctx context.Context, limit int) ([]events.Created, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("outbox limit must be between 1 and 1000")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT Payload FROM OutboxEvents WHERE PublishedAt IS NULL ORDER BY CreatedAt, EventId FETCH FIRST `+fmt.Sprint(limit)+` ROWS ONLY`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]events.Created, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		e, err := events.Decode([]byte(payload))
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func (s *Store) MarkPublished(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE OutboxEvents SET PublishedAt = :1 WHERE EventId = :2 AND PublishedAt IS NULL", time.Now().UTC(), id)
	return err
}

func (s *Store) Process(ctx context.Context, e events.Created) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var bookingID string
	err = tx.QueryRowContext(ctx, "SELECT BookingId FROM OutboxEvents WHERE EventId = :1 FOR UPDATE", e.EventID).Scan(&bookingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} // A demo reset discarded it.
	if err != nil {
		return err
	}
	if bookingID != e.BookingID {
		return fmt.Errorf("event booking ID does not match outbox")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO ProcessedEvents (EventId, BookingId, ProcessedAt) VALUES (:1,:2,:3)", e.EventID, e.BookingID, time.Now().UTC())
	if uniqueViolation(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
