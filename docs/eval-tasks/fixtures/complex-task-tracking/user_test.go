package users

import "testing"

func TestUsers(t *testing.T) {
	if got := CreateUser(" Alice "); got != "Alice" {
		t.Fatalf("CreateUser() = %q", got)
	}
	if got := RenameUser(" Bob "); got != "Bob" {
		t.Fatalf("RenameUser() = %q", got)
	}
}
