"""Exercise the actual CLI and official binaries in an isolated temporary directory."""
import argparse
import base64
import contextlib
import hashlib
import http.server
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time
import dns_path


def port(socktype=socket.SOCK_STREAM):
    with socket.socket(type=socktype) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


@contextlib.contextmanager
def running(args, cwd):
    with subprocess.Popen(args, cwd=cwd, stdout=subprocess.DEVNULL,
                          stderr=subprocess.DEVNULL) as proc:
        try:
            yield proc
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()


class Target(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Length", str(len(b"singforge-roundtrip")))
        self.end_headers()
        self.wfile.write(b"singforge-roundtrip")

    def log_message(self, *_):
        pass


def test_edge(a, lab, ws, cli, cert):
    # Replace the public edge with a local TLS origin and a private test trust anchor.
    p = port()
    conf = (ws / "current/nginx.conf").read_text()
    conf = conf.replace("listen 2053 ssl;", f"listen 127.0.0.1:{p} ssl;")
    conf = conf.replace("listen [::]:2053 ssl;", "")
    conf = conf.replace("/run/credentials/singforge-edge.service/", str(ws / "current") + "/")
    conf = conf.replace("/run/singforge-edge/", str(lab) + "/")
    (lab / "edge.conf").write_text(conf)
    target = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Target)
    threading.Thread(target=target.serve_forever, daemon=True).start()
    try:
        with running([a.core, "run", "-c", ws / "current/server.json"], lab), running(
                ["nginx", "-p", str(lab), "-c", lab / "edge.conf"], lab):
            for proto in ("vless-ws", "trojan-ws", "vmess-ws"):
                obj = json.loads(cli("subscription", "show", "--format", "sing-box",
                                     "--filter", "cdn-" + proto))
                local = port()
                obj["inbounds"][0]["listen_port"] = local
                out = obj["outbounds"][0]
                out["transport"]["headers"] = {"Host": out["server"]}
                out.update(server="127.0.0.1", server_port=p)
                out["tls"]["certificate"] = cert.read_text()
                del out["domain_resolver"]
                config = lab / "edge-client.json"
                config.write_text(json.dumps(obj))
                with running([a.core, "run", "-c", config], lab):
                    for _ in range(40):
                        try:
                            socket.create_connection(("127.0.0.1",local),timeout=.2).close()
                            break
                        except OSError:
                            time.sleep(.1)
                    data = subprocess.check_output(["curl","-fsS","--max-time","8","--noproxy","",
                        "-x",f"http://127.0.0.1:{local}",f"http://127.0.0.1:{target.server_port}/"])
                    assert data == b"singforge-roundtrip"
                print(f"PASS HTTPS Nginx path / {proto}",flush=True)
    finally:
        target.shutdown()
        target.server_close()

def suite(a, lab):
    ws = lab / "workspace"
    ws.mkdir(mode=0o700)

    def cli(*args, success=True):
        cmd = [a.cli, "--workspace", ws, "--core", a.core, *map(str, args)]
        result = subprocess.run(cmd, capture_output=True)
        if (result.returncode == 0) != success:
            raise RuntimeError(f"{args[:2]} failed expectation: {result.stderr.decode()[:400]}")
        return result.stdout

    cli("install", "--host", "127.0.0.1", "--listen", "127.0.0.1", "--preset", "full")
    state = lambda: json.loads((ws / "current/state.json").read_bytes())
    cli("rules","set","--profile","global")  # Loopback target must traverse the proxy.
    qr=lab/"node.png"
    cli("subscription","show","--format","shadowsocks","--filter","direct-ss","--qr","--output",qr)
    assert qr.read_bytes().startswith(bytes.fromhex("89504e470d0a1a0a")) and qr.stat().st_mode & 0o777 == 0o600
    cli("dns","set","--capture","system")  # Keep the test process off the host TUN.
    first = state()
    for inbound in first["Inbounds"]:
        socktype = socket.SOCK_DGRAM if inbound["Protocol"] in ("tuic", "hysteria2") else socket.SOCK_STREAM
        cli("protocol", "configure", "--id", inbound["Protocol"], "--port", port(socktype))
    cli("protocol", "disable", "--id", "ss")
    cli("protocol", "enable", "--id", "ss")
    assert [i["Password"] for i in first["Inbounds"]] == [i["Password"] for i in state()["Inbounds"]]
    cli("endpoint", "add", "--id", "ss", "--host", "::1", "--name", "ipv6-ss")
    assert b"@[::1]" in cli("subscription", "show", "--format", "uri", "--filter", "ipv6-ss")
    cli("endpoint", "remove", "--id", "ipv6-ss", "--yes")

    target = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Target)
    threading.Thread(target=target.serve_forever, daemon=True).start()
    try:
        with running([a.core, "run", "-c", ws / "current/server.json"], lab):
            for client, form in [(a.core, "sing-box"), (a.mihomo, "mihomo")]:
                for proto in ["ss", "ss2022", "trojan", "tuic", "hysteria2", "anytls", "snell"]:
                    if proto == "snell" and form == "mihomo":
                        continue
                    data = cli("subscription","show","--format",form,"--filter","direct-"+proto)
                    p = port()
                    cfg = lab / (form + ".conf")
                    if form == "mihomo":
                        cfg.write_bytes(data.replace(b"mixed-port: 7890",f"mixed-port: {p}".encode()).replace(b"127.0.0.1:1053",f"127.0.0.1:{port()}".encode()))
                        cmd = [client, "-d", str(lab), "-f", cfg]
                    else:
                        obj=json.loads(data)
                        obj["inbounds"][0]["listen_port"]=p
                        cfg.write_text(json.dumps(obj))
                        cmd = [client,"run","-c",cfg]
                    with running(cmd, lab) as proc:
                        for _ in range(40):
                            if proc.poll() is not None:
                                raise RuntimeError(f"{form} {proto} failed at startup")
                            try:
                                socket.create_connection(("127.0.0.1",p),timeout=.2).close()
                                break
                            except OSError:
                                time.sleep(.1)
                        result=subprocess.run(["curl","-fsS","--max-time","8","--noproxy","","--proxy",f"http://127.0.0.1:{p}",f"http://127.0.0.1:{target.server_port}/"],capture_output=True)
                        assert result.stdout==b"singforge-roundtrip", f"{form}/{proto} round trip failed ({result.stderr!r})"
                    print(f"PASS {form}/{proto} authenticated traffic",flush=True)
    finally:
        target.shutdown()
        target.server_close()
    cert,key=lab/"cert",lab/"key"
    subprocess.run(["openssl","req","-x509","-newkey","rsa:2048","-nodes","-days","1","-subj","/CN=cdn.example.test","-addext","subjectAltName=DNS:cdn.example.test,DNS:sub.example.test","-keyout",key,"-out",cert],check=True,capture_output=True)
    dns_path.check(a,lab,ws,cli,cert,key,running,port)
    credentials=lab/"tunnel.json"
    credentials.write_text(json.dumps({"TunnelID":"7e127f9a-8a77-4332-bacd-86a2636f76ea","AccountTag":"a"*32,"TunnelSecret":base64.b64encode(os.urandom(32)).decode()}))
    cli("cloudflare","enable","--mode","cdn","--host","cdn.example.test","--cert",cert,"--key",key)
    cli("cloudflare","enable","--mode","tunnel","--host","edge.example.test","--credentials",credentials)
    cli("protocol","enable","--id","vmess-ws")
    inbounds = state()["Inbounds"]
    cli("edge", "set", "--mode", "ips", "--ips", "127.0.0.1", success=False)
    preview = json.loads(cli("edge", "set", "--mode", "domains", "--dry-run"))
    assert len(preview["preferred_addresses"]) == 8
    assert json.loads(cli("edge", "status"))["mode"] == "default"
    cli("edge", "set", "--mode", "all", "--ips", "bestcf.top", success=False)
    cli("edge", "set", "--mode", "all")
    assert len(json.loads(cli("edge","status"))["preferred_addresses"]) == 10
    assert state()["Inbounds"] == inbounds
    assert b"Preferred Edge" in cli("subscription", "show", "--format", "mihomo")
    test_edge(a,lab,ws,cli,cert)
    if a.cloudflared:
        subprocess.run([a.cloudflared,"tunnel","--config",ws/"current/tunnel.yml","ingress","validate"],check=True)
    for form in ["sing-box","mihomo"]:
        cfg=lab/("hybrid-"+form)
        cfg.write_bytes(cli("subscription","show","--format",form))
        cmd=[a.core,"check","-c",cfg] if form=="sing-box" else [a.mihomo,"-t","-d",lab,"-f",cfg]
        subprocess.run(cmd,check=True)
    cli("subscription","publish","--host","sub.example.test","--key",key,"--cert",cert,"--ca",cert)
    one=cli("subscription","rotate").decode().strip()
    assert one.startswith("https://sub.example.test/s/")
    assert one.split("/")[4] not in (ws/"current/state.json").read_text()
    cli("subscription","revoke","--yes")
    backup=lab/"private.backup"
    cli("backup","create","--file",backup)
    cli("uninstall","--yes","--purge")
    cli("backup","restore","--file",backup,"--yes")
    assert state()["Inbounds"]==json.loads(backup.read_bytes())["State"]["Inbounds"]
    active=os.readlink(ws/"current")
    cli("core","update","--file",a.core,"--sha256","0"*64,success=False)
    assert os.readlink(ws/"current")==active
    cli("core","update","--file",a.core,"--sha256",hashlib.sha256(Path(a.core).read_bytes()).hexdigest())
    cli("core","rollback","--yes")
    print("PASS hybrid, publication, backup/restore and verified core replacement")

if __name__=="__main__":
    parser=argparse.ArgumentParser()
    for name in ("cli","core","mihomo","cloudflared"):
        parser.add_argument("--"+name,required=name!="cloudflared",type=lambda s:str(Path(s).resolve()))
    args=parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="sf-test-") as directory:
        suite(args,Path(directory))
