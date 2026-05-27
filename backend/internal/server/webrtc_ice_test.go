package server

import "testing"

func TestWebRTCICEServersIncludeStaticAuthTURN(t *testing.T) {
	t.Setenv("WEBRTC_STUN_URL", "stun:example.com:19302")
	t.Setenv("WEBRTC_TURN_URLS", "turn:one.example.com:80, turns:two.example.com:443?transport=tcp")
	t.Setenv("WEBRTC_TURN_STATIC_AUTH_SECRET", "test-secret")
	t.Setenv("WEBRTC_TURN_TTL_SECONDS", "600")

	srv := &Server{}
	servers := srv.webrtcICEServers()

	if len(servers) != 3 {
		t.Fatalf("server count = %d, want 3: %#v", len(servers), servers)
	}
	if servers[0]["urls"] != "stun:example.com:19302" {
		t.Fatalf("stun url = %v", servers[0]["urls"])
	}
	for i := 1; i < len(servers); i++ {
		if servers[i]["username"] == "" {
			t.Fatalf("turn server %d missing username: %#v", i, servers[i])
		}
		if servers[i]["credential"] == "" {
			t.Fatalf("turn server %d missing credential: %#v", i, servers[i])
		}
	}
}
