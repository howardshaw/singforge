"""Validate shared TCP/UDP listeners, a local TLS handshake site, and optional WS routes."""
import argparse
import base64
import http.server
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time
from integration import Target, port, running

def exercise(a,lab):
    ws=lab/"ws"
    ws.mkdir(mode=0o700)
    def cli(*args):
        return subprocess.check_output([a.cli,"--workspace",ws,"--core",a.core,*map(str,args)])
    public=a.port or port()
    backend=port()
    name="origin.example.test"
    cert,key=lab/"cert.pem",lab/"key.pem"
    subprocess.run(["openssl","req","-x509","-newkey","rsa:2048","-nodes","-days","1","-subj","/CN="+name,
                    "-addext","subjectAltName=DNS:"+name,"-keyout",key,"-out",cert],check=True,capture_output=True)
    site=lab/"nginx.conf"
    site.write_text(f"""daemon off; pid {lab}/nginx.pid; error_log /dev/null; events {{}} http {{
        access_log off; client_body_temp_path {lab}/body; proxy_temp_path {lab}/proxy;
        fastcgi_temp_path {lab}/fastcgi; uwsgi_temp_path {lab}/uwsgi; scgi_temp_path {lab}/scgi;
        server {{listen 127.0.0.1:{backend} ssl; server_name {name}; ssl_protocols TLSv1.3;
        ssl_certificate {cert}; ssl_certificate_key {key}; location / {{return 200 "TLS origin";}}}} }}""")
    cli("install","--preset","custom","--protocols","reality,hysteria2","--host","127.0.0.1","--listen","127.0.0.1",
        "--handshake",name,"--handshake-target",f"127.0.0.1:{backend}")
    for proto in ("reality","hysteria2"):
        cli("protocol","configure","--id",proto,"--port",public)
    cli("rules","set","--profile","global")
    target=http.server.ThreadingHTTPServer(("127.0.0.1",0),Target)
    threading.Thread(target=target.serve_forever,daemon=True).start()
    try:
        with running(["nginx","-p",lab,"-c",site],lab), running([a.core,"run","-c",ws/"current/server.json"],lab):
            time.sleep(1)
            page=subprocess.check_output(["curl","--retry","4","--retry-connrefused","--max-time","10","-fsS","--noproxy","*","--cacert",cert,
                                         "--resolve",f"{name}:{public}:127.0.0.1",f"https://{name}:{public}/"])
            assert page==b"TLS origin"
            print("PASS ordinary HTTPS through Reality to TLS origin",flush=True)
            for proto in ("reality","hysteria2"):
                cfg=json.loads(cli("subscription","show","--format","sing-box","--filter","direct-"+proto))
                local=port()
                cfg["inbounds"][0]["listen_port"]=local
                client=lab/"client.json"
                client.write_text(json.dumps(cfg))
                with running([a.core,"run","-c",client],lab):
                    data=subprocess.check_output(["curl","--retry","4","--retry-connrefused","-fsS","--max-time","12","--noproxy","",
                        "-x",f"http://127.0.0.1:{local}",f"http://127.0.0.1:{target.server_port}/"])
                    assert data==b"singforge-roundtrip"
                print("PASS",proto,"authenticated traffic on",public,flush=True)
    finally:
        target.shutdown()
        target.server_close()
    creds=lab/"tunnel.json"
    creds.write_text(json.dumps({"TunnelID":"7e127f9a-8a77-4332-bacd-86a2636f76ea","AccountTag":"a"*32,
                                 "TunnelSecret":base64.b64encode(os.urandom(32)).decode()}))
    cli("protocol","disable","--id","hysteria2")
    cli("cloudflare","enable","--mode","tunnel","--host","edge.example.test","--credentials",creds)
    def selected():
        cfg=json.loads(cli("subscription","show","--format","sing-box"))
        return set(next(o for o in cfg["outbounds"] if o["tag"]=="Main")["outbounds"])
    assert selected()=={"direct-reality","tunnel-vless-ws","tunnel-trojan-ws"}
    cli("protocol","enable","--id","hysteria2")
    assert selected()=={"direct-reality","direct-hysteria2","tunnel-vless-ws","tunnel-trojan-ws"}
    if a.cloudflared:
        state=json.loads((ws/"current/state.json").read_bytes())
        for ib in state["Inbounds"]:
            if ib["Protocol"].endswith("-ws"):
                out=subprocess.check_output([a.cloudflared,"tunnel","--config",ws/"current/tunnel.yml","ingress","rule",
                                            "https://edge.example.test"+ib["Path"]],stderr=subprocess.STDOUT)
                assert f'127.0.0.1:{ib["Port"]}'.encode() in out
    print("PASS both optional tunnel topologies and selected routes",flush=True)

if __name__=="__main__":
    p=argparse.ArgumentParser()
    for k in ("cli","core","cloudflared"):
        p.add_argument("--"+k,required=k!="cloudflared",type=lambda s:str(Path(s).resolve()))
    p.add_argument("--port",type=int)
    with tempfile.TemporaryDirectory() as lab:
        exercise(p.parse_args(),Path(lab))
