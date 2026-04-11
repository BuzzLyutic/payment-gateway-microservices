package loader_test

import (
    "os"
    "testing"

    "github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
    "github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/loader"
)

func writeTempFile(t *testing.T, content string) string {
    t.Helper()
    f, err := os.CreateTemp("", "rules-*.json")
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { os.Remove(f.Name()) })
    if _, err := f.WriteString(content); err != nil {
        t.Fatal(err)
    }
    f.Close()
    return f.Name()
}

func TestLoad_DefaultRules(t *testing.T) {
    rules, err := loader.Load("../../rules/default.json")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(rules) != 6 {
        t.Fatalf("expected 6 rules, got %d", len(rules))
    }
}

func TestLoad_SimpleRule_GT(t *testing.T) {
    path := writeTempFile(t, `{
        "rules": [{
            "name": "test_amount",
            "type": "simple",
            "field": "amount",
            "operator": "gt",
            "value": 1000,
            "score": 10
        }]
    }`)

    rules, err := loader.Load(path)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    r := rules[0]
    if r.Name != "test_amount" {
        t.Errorf("expected name %q, got %q", "test_amount", r.Name)
    }
    if r.Type != domain.RuleTypeSimple {
        t.Errorf("expected type simple, got %q", r.Type)
    }
    if r.RawValue.Single == nil || *r.RawValue.Single != 1000 {
        t.Errorf("expected value 1000, got %v", r.RawValue.Single)
    }
}

func TestLoad_SimpleRule_Between(t *testing.T) {
    path := writeTempFile(t, `{
        "rules": [{
            "name": "night_time",
            "type": "simple",
            "field": "hour",
            "operator": "between",
            "value": [1, 5],
            "score": 10
        }]
    }`)

    rules, err := loader.Load(path)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    r := rules[0]
    if !r.RawValue.IsList {
        t.Error("expected IsList=true for between operator")
    }
    if r.RawValue.Range[0] != 1 || r.RawValue.Range[1] != 5 {
        t.Errorf("expected range [1,5], got %v", r.RawValue.Range)
    }
}

func TestLoad_VelocityRule(t *testing.T) {
    path := writeTempFile(t, `{
        "rules": [{
            "name": "velocity_merchant",
            "type": "velocity",
            "key_field": "merchant_id",
            "window": "10m",
            "threshold": 5,
            "score": 25
        }]
    }`)

    rules, err := loader.Load(path)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    r := rules[0]
    if r.KeyField != "merchant_id" {
        t.Errorf("expected key_field merchant_id, got %q", r.KeyField)
    }
    if r.Threshold != 5 {
        t.Errorf("expected threshold 5, got %d", r.Threshold)
    }
}

func TestLoad_Errors(t *testing.T) {
    cases := []struct {
        name    string
        content string
    }{
        {
            name:    "file not found",
            content: "",
        },
        {
            name: "unknown rule type",
            content: `{"rules": [{"name": "x", "type": "magic", "score": 10}]}`,
        },
        {
            name: "unknown field",
            content: `{"rules": [{"name": "x", "type": "simple", 
                "field": "unknown", "operator": "gt", "value": 1, "score": 10}]}`,
        },
        {
            name: "between wrong value",
            content: `{"rules": [{"name": "x", "type": "simple",
                "field": "hour", "operator": "between", "value": 5, "score": 10}]}`,
        },
        {
            name: "between min >= max",
            content: `{"rules": [{"name": "x", "type": "simple",
                "field": "hour", "operator": "between", "value": [5, 1], "score": 10}]}`,
        },
        {
            name: "velocity missing window",
            content: `{"rules": [{"name": "x", "type": "velocity",
                "key_field": "merchant_id", "threshold": 5, "score": 10}]}`,
        },
        {
            name: "velocity invalid window",
            content: `{"rules": [{"name": "x", "type": "velocity",
                "key_field": "merchant_id", "window": "invalid", 
                "threshold": 5, "score": 10}]}`,
        },
        {
            name: "empty rules",
            content: `{"rules": []}`,
        },
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            var path string
            if tc.content == "" {
                path = "/nonexistent/path/rules.json"
            } else {
                path = writeTempFile(t, tc.content)
            }

            _, err := loader.Load(path)
            if err == nil {
                t.Error("expected error, got nil")
            }
        })
    }
}
