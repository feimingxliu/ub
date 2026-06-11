package permission

import "testing"

func TestIsBlacklistedDestructiveVariants(t *testing.T) {
	t.Setenv("HOME", "/home/ub")
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "multi spaces", command: "rm    -rf    /", want: true},
		{name: "split flags", command: "rm -r -f /", want: true},
		{name: "escaped slash", command: `rm -rf \/`, want: true},
		{name: "home variable quoted", command: `rm -fr "$HOME"`, want: true},
		{name: "home variable braces", command: `rm --recursive --force ${HOME}/.cache`, want: true},
		{name: "sudo prefix absolute path", command: "sudo rm -rf /tmp/ub", want: true},
		{name: "mkfs", command: "mkfs.ext4 /dev/sda", want: true},
		{name: "dd device output", command: "dd if=file of=/dev/sda", want: true},
		{name: "relative build cleanup", command: "rm -rf ./build", want: false},
		{name: "fullwidth command", command: "ｒｍ -rf /", want: false},
		{name: "fullwidth slash", command: "rm -rf ／", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBlacklisted(tc.command); got != tc.want {
				t.Fatalf("isBlacklisted(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func FuzzIsBlacklisted(f *testing.F) {
	for _, seed := range []string{
		"rm -rf /",
		"rm    -r    -f    /",
		`rm -rf \/`,
		`rm -fr "$HOME"`,
		"mkfs.ext4 /dev/sda",
		"dd if=file of=/dev/sda",
		"ｒｍ -rf /",
		"rm -rf ／",
		"rm -rf ./build",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, command string) {
		first := isBlacklisted(command)
		second := isBlacklisted(command)
		if first != second {
			t.Fatalf("isBlacklisted is not deterministic for %q: %v then %v", command, first, second)
		}
	})
}
