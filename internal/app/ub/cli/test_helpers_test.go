package cli

import (
	"bytes"

	"github.com/spf13/cobra"
)

type testCommand struct {
	cmd *cobra.Command
	out *bytes.Buffer
	err *bytes.Buffer
}

type testRun struct {
	code int
	out  *bytes.Buffer
	err  *bytes.Buffer
}

func newTestRootCommand(args ...string) testCommand {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	if args != nil {
		cmd.SetArgs(args)
	}
	return testCommand{cmd: cmd, out: out, err: errOut}
}

func runCLITest(args ...string) testRun {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := Run(args, out, errOut)
	return testRun{code: code, out: out, err: errOut}
}
