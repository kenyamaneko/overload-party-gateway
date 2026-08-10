//go:build integration

package repository_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"cloud.google.com/go/firestore"

	"github.com/kenyamaneko/overload-party-gateway/internal/repository/postgrestest"
)

var (
	sharedPg              *postgrestest.Postgres
	sharedFirestoreClient *firestore.Client
)

func TestMain(m *testing.M) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT_ID")
	if emulatorHost == "" || projectID == "" {
		fmt.Fprintln(os.Stderr, "repository_test: FIRESTORE_EMULATOR_HOST と GOOGLE_CLOUD_PROJECT_ID の両方が必要です")
		os.Exit(1)
	}

	client, err := firestore.NewClient(context.Background(), projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "repository_test: firestore client: %v\n", err)
		os.Exit(1)
	}
	sharedFirestoreClient = client

	os.Exit(postgrestest.RunMain(m, &sharedPg))
}
