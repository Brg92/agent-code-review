package cli

import "testing"

func TestMsysPtyName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// What mintty (Git Bash), MSYS2 and Cygwin name the pipes of a terminal.
		{`\msys-1888ae32e00d56aa-pty0-from-master`, true},
		{`\msys-1888ae32e00d56aa-pty0-to-master`, true},
		{`\cygwin-e022582115c10879-pty3-from-master`, true},
		{`\cygwin-e022582115c10879-pty12-to-master`, true},
		// A pipe between two programs, or any other name.
		{``, false},
		{`\Win32Pipes.00000b04.00000001`, false},
		{`\msys-1888ae32e00d56aa-pty0`, false},
		{`\msys-1888ae32e00d56aa-pty0-from-master-extra`, false},
		{`\msys-1888ae32e00d56aa-pty-from-master`, false},
		{`\msys--pty0-from-master`, false},
		{`\msys-XYZ-pty0-from-master`, false},
		{`\mingw-1888ae32e00d56aa-pty0-from-master`, false},
		{`msys-1888ae32e00d56aa-pty0-from-master`, false},
		{`\\msys-1888ae32e00d56aa-pty0-from-master`, false},
		{`\foo\msys-1888ae32e00d56aa-pty0-from-master`, false},
		{"\\msys-1888ae32e00d56aa-pty0-from-master\n", false},
	}
	for _, tt := range tests {
		if got := msysPtyName(tt.name); got != tt.want {
			t.Errorf("msysPtyName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
