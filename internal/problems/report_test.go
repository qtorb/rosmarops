package problems

import (
	"encoding/json"
	"strings"
	"testing"
)

func marshalRaw(r Report) ([]byte, error) { return json.Marshal(r) }

// The literal example from §4 of the proposal must validate.
const docExample = `{"format":"problems.v1","lang":"es","rosmarops_version":"0.10.3","os":"windows","os_version":"10.0.26100","arch":"amd64",
 "problems":[{"code":"space_rejected","step":"space_send","http_status":409,"day":"2026-11-09","count":3},
             {"code":"config_unreadable","step":"agents","agent":"cline","day":"2026-11-09","count":1}],
 "comment":null,"email":null}`

func TestDocExampleValidates(t *testing.T) {
	if err := Validate([]byte(docExample)); err != nil {
		t.Fatalf("§4 example should validate: %v", err)
	}
}

func TestBuildReportValidates(t *testing.T) {
	sys := System{Lang: "en", Version: "0.10.3", OS: "linux", OSVersion: "", Arch: "arm64"}
	entries := []Entry{
		{Code: StepFailed, Step: "mcp", Day: "2026-11-09", Count: 1},
		{Code: ConfigUnreadable, Step: StepAgents, Agent: "cline", Day: "2026-11-09", Count: 2},
		{Code: SpaceRejected, Step: StepSpaceSend, HTTPStatus: 409, Day: "2026-11-09", Count: 3},
	}
	comment := "something broke"
	email := "me@example.com"
	b, err := Marshal(BuildReport(sys, entries, &comment, &email))
	if err != nil {
		t.Fatalf("report should marshal and validate: %v", err)
	}
	if len(b) > MaxBodyBytes {
		t.Fatalf("body %d exceeds %d", len(b), MaxBodyBytes)
	}
	// os_version may be empty (pattern allows length 0).
	if err := Validate(b); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestReportRejectsBadValues(t *testing.T) {
	sys := System{Lang: "en", Version: "0.10.3", OS: "linux", Arch: "amd64"}
	good := Entry{Code: StepFailed, Step: "mcp", Day: "2026-11-09", Count: 1}

	t.Run("more than 20 problems", func(t *testing.T) {
		var many []Entry
		for i := 0; i < 21; i++ {
			many = append(many, good)
		}
		if _, err := Marshal(BuildReport(sys, many, nil, nil)); err == nil {
			t.Fatal("want error for 21 problems")
		}
	})
	t.Run("empty problems", func(t *testing.T) {
		if _, err := Marshal(BuildReport(sys, nil, nil, nil)); err == nil {
			t.Fatal("want error for 0 problems")
		}
	})
	t.Run("email without @", func(t *testing.T) {
		bad := "notanemail"
		if _, err := Marshal(BuildReport(sys, []Entry{good}, nil, &bad)); err == nil {
			t.Fatal("want error for email without @")
		}
	})
	t.Run("unknown lang", func(t *testing.T) {
		b := BuildReport(sys, []Entry{good}, nil, nil)
		b.Lang = "fr"
		raw, _ := marshalRaw(b)
		if err := Validate(raw); err == nil {
			t.Fatal("want error for lang fr")
		}
	})
	t.Run("version with space", func(t *testing.T) {
		b := BuildReport(sys, []Entry{good}, nil, nil)
		b.RosmaropsVersion = "0.10 3"
		raw, _ := marshalRaw(b)
		if err := Validate(raw); err == nil {
			t.Fatal("want error for version with a space")
		}
	})
}

func TestNullCommentEmailAllowed(t *testing.T) {
	sys := System{Lang: "es", Version: "0.10.3", OS: "darwin", Arch: "arm64"}
	b, err := Marshal(BuildReport(sys, []Entry{{Code: TimeRanOut, Step: StepReview, Day: "2026-11-09", Count: 1}}, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"comment":null`) || !strings.Contains(string(b), `"email":null`) {
		t.Fatalf("nil comment/email should encode as null: %s", b)
	}
}
