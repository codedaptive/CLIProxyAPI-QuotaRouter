package config

import "testing"

func TestRoleConfigurationValidationAndRetention(t *testing.T) {
	raw := []byte(`mode: active
roles:
 - name: designer
   enabled: true
   targets:
    - model: claude-opus-5-5
      effort: high
    - model: gpt-6.1-sol
      effort: xhigh
      providers: [codex]
      account_ids: [pro-account.json]
      effort_map: {max: xhigh}
`)
	c, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err = DecodeJSONPatch(c, []byte(`{"strategy":"balanced"}`))
	if err != nil || len(c.Roles) != 1 || len(c.Roles[0].Targets) != 2 {
		t.Fatalf("lost role config: %+v %v", c.Roles, err)
	}
	for _, patch := range []string{
		`{"roles":[{"name":"designer","targets":[{"model":"designer","effort":"high"}]}]}`,
		`{"roles":[{"name":"designer","targets":[{"model":"opus(high)","effort":"high"}]}]}`,
		`{"roles":[{"name":"designer","targets":[{"model":"opus","effort":"bogus"}]}]}`,
		`{"roles":[{"name":"designer","targets":[]}]}`,
	} {
		if _, err := DecodeJSONPatch(c, []byte(patch)); err == nil {
			t.Fatalf("accepted %s", patch)
		}
	}
	c, err = DecodeJSONPatch(c, []byte(`{"roles":[]}`))
	if err != nil || len(c.Roles) != 0 {
		t.Fatalf("cannot delete roles: %v", err)
	}
}
