package main

import "testing"

func TestReadConfig(t *testing.T) {
	for _, tc := range []struct {
		name, port, host, want string
		invalid                bool
	}{
		{name: "значения по умолчанию", want: ":8080"},
		{name: "локальный IPv4", port: "9090", host: "127.0.0.1", want: "127.0.0.1:9090"},
		{name: "IPv6", port: "8081", host: "::1", want: "[::1]:8081"},
		{name: "максимальный порт", port: "65535", want: ":65535"},
		{name: "ноль", port: "0", invalid: true},
		{name: "отрицательный", port: "-1", invalid: true},
		{name: "слишком большой", port: "65536", invalid: true},
		{name: "название сервиса", port: "http", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PORT", tc.port)
			t.Setenv("HOST", tc.host)
			t.Setenv("API_BASE_URL", "")
			cfg, err := readConfig()
			if tc.invalid {
				if err == nil {
					t.Fatal("неверный порт принят")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.address != tc.want {
				t.Fatalf("адрес %q, ожидался %q", cfg.address, tc.want)
			}
			if cfg.apiURL != "https://groupietrackers.herokuapp.com/api" {
				t.Fatal("неверный API по умолчанию")
			}
		})
	}
}
