package link

import (
	"context"
	"testing"
)

func TestServiceWithFirstID(t *testing.T) {
	tests := []struct {
		name      string
		opts      []Option
		wantFirst uint64
		wantCodes [2]string
	}{
		{
			name:      "default starts at one",
			wantFirst: 1,
			wantCodes: [2]string{"000001", "000002"},
		},
		{
			name:      "zero is ignored",
			opts:      []Option{WithFirstID(0)},
			wantFirst: 1,
			wantCodes: [2]string{"000001", "000002"},
		},
		{
			name:      "one is accepted",
			opts:      []Option{WithFirstID(1)},
			wantFirst: 1,
			wantCodes: [2]string{"000001", "000002"},
		},
		{
			name:      "custom starting ID",
			opts:      []Option{WithFirstID(62)},
			wantFirst: 62,
			wantCodes: [2]string{"000010", "000011"},
		},
		{
			name:      "zero preserves an earlier option",
			opts:      []Option{WithFirstID(62), WithFirstID(0)},
			wantFirst: 62,
			wantCodes: [2]string{"000010", "000011"},
		},
		{
			name:      "last valid option wins",
			opts:      []Option{WithFirstID(62), WithFirstID(61)},
			wantFirst: 61,
			wantCodes: [2]string{"00000z", "000010"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(newHW2Store(), tc.opts...)
			for i, wantCode := range tc.wantCodes {
				got, err := svc.Shorten(context.Background(), ShortenRequest{
					Target:  "https://example.com",
					OwnerID: "owner",
				})
				if err != nil {
					t.Fatalf("Shorten call %d: %v", i+1, err)
				}
				if wantID := tc.wantFirst + uint64(i); got.ID != wantID || got.Code != wantCode {
					t.Errorf("Shorten call %d: ID = %d, Code = %q; want ID = %d, Code = %q",
						i+1, got.ID, got.Code, wantID, wantCode)
				}
			}
		})
	}
}
