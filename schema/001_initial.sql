CREATE TABLE Hotels (
  HotelId STRING(36) NOT NULL,
  Name STRING(200) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL
) PRIMARY KEY (HotelId);

CREATE UNIQUE INDEX HotelsByName ON Hotels(Name);

CREATE TABLE Rooms (
  RoomId STRING(36) NOT NULL,
  HotelId STRING(36) NOT NULL,
  RoomNumber STRING(10) NOT NULL,
  RoomType STRING(16) NOT NULL,
  Capacity INT64 NOT NULL
) PRIMARY KEY (RoomId);

CREATE UNIQUE INDEX RoomsByHotelAndNumber ON Rooms(HotelId, RoomNumber);

CREATE TABLE Bookings (
  BookingId STRING(36) NOT NULL,
  Reference STRING(35) NOT NULL,
  HotelId STRING(36) NOT NULL,
  RoomId STRING(36) NOT NULL,
  GuestName STRING(200) NOT NULL,
  GuestCount INT64 NOT NULL,
  CheckInDate DATE NOT NULL,
  CheckOutDate DATE NOT NULL,
  CreatedAt TIMESTAMP NOT NULL
) PRIMARY KEY (BookingId);

CREATE UNIQUE INDEX BookingsByReference ON Bookings(Reference);

CREATE TABLE OccupiedNights (
  RoomId STRING(36) NOT NULL,
  StayDate DATE NOT NULL,
  BookingId STRING(36) NOT NULL
) PRIMARY KEY (RoomId, StayDate);

CREATE TABLE OutboxEvents (
  EventId STRING(36) NOT NULL,
  BookingId STRING(36) NOT NULL,
  Payload STRING(MAX) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL,
  PublishedAt TIMESTAMP
) PRIMARY KEY (EventId);

CREATE TABLE ProcessedEvents (
  EventId STRING(36) NOT NULL,
  BookingId STRING(36) NOT NULL,
  ProcessedAt TIMESTAMP NOT NULL
) PRIMARY KEY (EventId);
