package controllers

import "testing"

func TestRelayAllowsAnonymous(t *testing.T) {
        tests := []struct {
                name string
                rule RelayRule
                want bool
        }{
                {
                        name: "exact ygg prefix",
                        rule: RelayRule{Dest: "/yggdrasil-api"},
                        want: true,
                },
                {
                        name: "nested ygg path",
                        rule: RelayRule{Dest: "/yggdrasil-api/authserver"},
                        want: true,
                },
                {
                        name: "other service path",
                        rule: RelayRule{Dest: "/services/sdk/foo"},
                        want: false,
                },
                {
                        name: "legacy direct authserver path",
                        rule: RelayRule{Dest: "/authserver"},
                        want: false,
                },
        }

        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        if got := relayAllowsAnonymous(tt.rule); got != tt.want {
                                t.Fatalf("relayAllowsAnonymous(%q) = %v, want %v", tt.rule.Dest, got, tt.want)
                        }
                })
        }
}
