"""SSH-style PTY smoke. Pass --ssh HOST to inspect an installed deployment."""
import argparse
import fcntl
import os
import pty
import select
import struct
import sys
import termios
import time

def smoke(args):
    pid, fd = pty.fork()
    if pid == 0:
        fcntl.ioctl(0,termios.TIOCSWINSZ,struct.pack("HHHH",22,65,0,0))
        env={**os.environ,"TERM":"xterm-256color","SINGFORGE_LANG":"en"}
        cmd=([args.cli] if not args.ssh else ["ssh","-tt","-o","BatchMode=yes",args.ssh,
             "sudo env TERM=xterm-256color SINGFORGE_LANG=en singforge"])
        os.execvpe(cmd[0],cmd,env)
    raw=bytearray()
    def wait(text):
        deadline=time.monotonic()+25
        while time.monotonic()<deadline:
            if text.encode() in raw:
                raw.clear()
                return
            if select.select([fd],[],[],.25)[0]:
                raw.extend(os.read(fd,65536))
        raise RuntimeError("terminal never displayed "+text+"; tail="+repr(bytes(raw[-400:])))
    try:
        wait("Server management")
        os.write(fd,b"\x1b[B")
        time.sleep(.15)
        if not args.ssh:
            os.write(fd,b"\x1b[B")
            time.sleep(.15)
        os.write(fd,b"\r")
        wait("direct-ss" if args.ssh else "hysteria2")
        os.write(fd,b"\x1b")
        wait("Server management")
        if not args.ssh:
            # From the home screen the last three entries are All commands, Help, Exit.
            os.write(fd,b"\x1b[F")
            time.sleep(.15)
            os.write(fd,b"\x1b[A\x1b[A\r")
            wait("All commands")
            os.write(fd,b"\x1b[B\r")
            wait("disable")
            os.write(fd,b"\x1b[F\x1b[A\r")
            wait("Review and run")
            os.write(fd,b"\x1b[F\r")
            wait("singforge bbr status")
            for title in ("Review and run", "disable", "All commands", "Server management"):
                os.write(fd,b"\x1b")
                wait(title)
        os.write(fd,b"l")
        wait("服务器管理")
        os.write(fd,b"q")
        deadline=time.monotonic()+25
        while time.monotonic()<deadline:
            done,code=os.waitpid(pid,os.WNOHANG)
            if done:
                assert os.waitstatus_to_exitcode(code)==0
                print("PASS vertical TUI, arrow navigation, real detail view, Esc, language switch and quit")
                return
            if select.select([fd],[],[],.2)[0]:
                try: raw.extend(os.read(fd,65536))
                except OSError: time.sleep(.1)
        raise RuntimeError("terminal did not exit: "+repr(bytes(raw[-200:])))
    finally:
        os.close(fd)
        try: os.kill(pid,15)
        except ProcessLookupError: pass
if __name__=="__main__":
    p=argparse.ArgumentParser()
    p.add_argument("--cli",default="./bin/singforge")
    p.add_argument("--ssh")
    smoke(p.parse_args())
