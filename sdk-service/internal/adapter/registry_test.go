package adapter

import (
    "errors"
    "testing"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type testAdapter interface { Execute() }
type testImplementation struct{}
func (*testImplementation) Execute() {}

func TestRegistryValidatesEntries(t *testing.T) {
    var typedNil *testImplementation
    for _, tc := range []struct {
        name string
        input map[string]testAdapter
    }{
        {"empty", nil},
        {"blank name", map[string]testAdapter{"": &testImplementation{}}},
        {"nil interface", map[string]testAdapter{"standard": nil}},
        {"typed nil", map[string]testAdapter{"standard": typedNil}},
    } {
        t.Run(tc.name, func(t *testing.T) {
            _, err := NewRegistry(tc.input)
            if !errors.Is(err, model.ErrAdapterMissing) {
                t.Fatalf("expected ErrAdapterMissing, got %v", err)
            }
        })
    }
}

func TestRegistryCopiesInputAndRejectsUnknown(t *testing.T) {
    implementation := &testImplementation{}
    input := map[string]testAdapter{"standard": implementation}
    registry, err := NewRegistry(input)
    if err != nil { t.Fatal(err) }
    delete(input, "standard")
    got, err := registry.Get("standard")
    if err != nil || got != implementation {
        t.Fatalf("registry must keep original adapter: %v", err)
    }
    if _, err := registry.Get("unknown"); !errors.Is(err, model.ErrAdapterMissing) {
        t.Fatalf("unknown adapter must fail: %v", err)
    }
    var nilRegistry *Registry[testAdapter]
    if _, err := nilRegistry.Get("standard"); !errors.Is(err, model.ErrAdapterMissing) {
        t.Fatalf("nil registry must fail: %v", err)
    }
}
