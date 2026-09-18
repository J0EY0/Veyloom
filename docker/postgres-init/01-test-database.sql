-- Runs once, when the Postgres volume is first created. The dev database
-- (veyloom) is the person's own; tests and automated checks use
-- veyloom_test, so nothing they create shows up in the real rooms.
CREATE DATABASE veyloom_test;
