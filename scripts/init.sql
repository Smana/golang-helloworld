-- init.sql
CREATE DATABASE wordsdb;
\c wordsdb;
CREATE TABLE IF NOT EXISTS words (
    id SERIAL PRIMARY KEY,
    word VARCHAR(255) NOT NULL
);