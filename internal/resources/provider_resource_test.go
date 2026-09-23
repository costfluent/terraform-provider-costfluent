package resources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
)

func pending() error {
	return &costfluent.APIError{StatusCode: 400, Errors: []costfluent.FieldError{
		{Name: "credentials", Reason: "not shared yet", Code: costfluent.CodeGcpAccessPending},
	}}
}

func TestCreateProviderRetriesGcpAccessPendingUntilItConnects(t *testing.T) {
	calls := 0
	created, err := createProviderRetryingAccessPending(context.Background(), func() (*costfluent.ProviderCreated, error) {
		calls++
		if calls < 3 {
			return nil, pending()
		}
		return &costfluent.ProviderCreated{ID: "prv_1"}, nil
	}, time.Millisecond, time.Second)

	if err != nil || created.ID != "prv_1" || calls != 3 {
		t.Fatalf("created %+v, err %v, calls %d", created, err, calls)
	}
}

func TestCreateProviderRetriesAwsAccessPendingUntilItConnects(t *testing.T) {
	calls := 0
	created, err := createProviderRetryingAccessPending(context.Background(), func() (*costfluent.ProviderCreated, error) {
		calls++
		if calls < 2 {
			return nil, &costfluent.APIError{StatusCode: 400, Errors: []costfluent.FieldError{
				{Name: "credentials", Reason: "role not assumable yet", Code: costfluent.CodeAwsAccessPending},
			}}
		}
		return &costfluent.ProviderCreated{ID: "prv_aws"}, nil
	}, time.Millisecond, time.Second)

	if err != nil || created.ID != "prv_aws" || calls != 2 {
		t.Fatalf("created %+v, err %v, calls %d", created, err, calls)
	}
}

func TestCreateProviderDoesNotRetryOtherFailures(t *testing.T) {
	calls := 0
	_, err := createProviderRetryingAccessPending(context.Background(), func() (*costfluent.ProviderCreated, error) {
		calls++
		return nil, &costfluent.APIError{StatusCode: 400, Detail: "invalid billing account"}
	}, time.Millisecond, time.Second)

	if err == nil || calls != 1 {
		t.Fatalf("err %v, calls %d", err, calls)
	}
}

func TestCreateProviderGivesUpAtTheDeadline(t *testing.T) {
	calls := 0
	_, err := createProviderRetryingAccessPending(context.Background(), func() (*costfluent.ProviderCreated, error) {
		calls++
		return nil, pending()
	}, 10*time.Millisecond, 35*time.Millisecond)

	if !costfluent.HasCode(err, costfluent.CodeGcpAccessPending) || calls < 2 || calls > 4 {
		t.Fatalf("err %v, calls %d", err, calls)
	}
}

func TestCreateProviderStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := createProviderRetryingAccessPending(ctx, func() (*costfluent.ProviderCreated, error) {
		return nil, pending()
	}, time.Hour, 3*time.Hour)

	var apiErr *costfluent.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err %v", err)
	}
}
