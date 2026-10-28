package config

import "testing"

func TestProfileLifecycle(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ClearCache()

	if err := StoreProfile("work", Profile{Kind: KindPAT, Seat: "full"}, false); err != nil {
		t.Fatal(err)
	}
	if err := StoreProfile("other", Profile{Kind: KindPAT}, false); err != nil {
		t.Fatal(err)
	}
	cfg := Read()
	if cfg.DefaultProfile != "work" {
		t.Fatalf("first profile should become default, got %q", cfg.DefaultProfile)
	}
	if err := UpdateProfile("work", func(p *Profile) { p.Plan = "org" }); err != nil {
		t.Fatal(err)
	}
	if got := Read().Profiles["work"].Plan; got != "org" {
		t.Fatalf("plan = %q", got)
	}
	if err := UpdateProfile("missing", func(*Profile) {}); err == nil {
		t.Fatal("updating a missing profile should fail")
	}
	if err := RemoveProfile("work"); err != nil {
		t.Fatal(err)
	}
	if got := Read().DefaultProfile; got != "other" {
		t.Fatalf("default after removal = %q", got)
	}
}

func TestEffectiveDefaults(t *testing.T) {
	var d Defaults
	if d.EffectiveMaxWaitS() != DefaultMaxWaitS || d.EffectivePrefetch() != PrefetchRelated {
		t.Fatal("unset defaults should fall back to built-ins")
	}
	zero := 0
	d.MaxRetries = &zero
	if d.EffectiveMaxRetries() != 0 {
		t.Fatal("an explicit zero must be kept")
	}
}
