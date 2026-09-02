-- Migration 002: persist camera stream availability.
ALTER TABLE cameras ADD COLUMN available INTEGER NOT NULL DEFAULT 0;
