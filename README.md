# Helloworld Go Web App

This is a simple Go web application that responds with "Hello, World!" when accessed. It also includes a `/store` endpoint that accepts a word via a POST request and stores it in a PostgreSQL database. It is designed to be containerizable using Docker.

## Prerequisites

- Docker
- Docker Compose

## Running the Web App

1. Create a `docker-compose.yml` file with the following content:

    ```yaml
    version: '3.8'

    services:
      db:
        image: postgres:13
        environment:
          POSTGRES_USER: user
          POSTGRES_PASSWORD: password
          POSTGRES_DB: wordsdb
        ports:
          - "5432:5432"
        volumes:
          - db_data:/var/lib/postgresql/data

      app:
        build: .
        environment:
          DATABASE_URL: postgres://user:password@db:5432/wordsdb?sslmode=disable
        ports:
          - "8080:8080"
        depends_on:
          - db

    volumes:
      db_data:
    ```

2. Build and run the containers:

    ```sh
    docker-compose up --build
    ```

3. Access the web app by navigating to `http://localhost:8080` in your web browser.

4. Store a word using a `curl` POST request:

    ```sh
    curl -X POST -d '{"word":"example"}' -H "Content-Type: application/json" http://localhost:8080/store
    ```

## Project Structure

- `cmd/helloworld/main.go`: Entry point of the application.
- `internal/server/handler.go`: Contains the HTTP handlers.
- `internal/server/db.go`: Contains the database connection logic.
- `Dockerfile`: Docker configuration for containerizing the application.
- `go.mod`: Go module dependencies.
- `README.md`: Instructions on how to build and run the application.
