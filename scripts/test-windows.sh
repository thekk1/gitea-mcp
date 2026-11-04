#!/usr/bin/env bash
set -euo pipefail

image="${WINDOWS_TEST_IMAGE:?}"
timeout="${WINDOWS_TEST_TIMEOUT:-165}"
cache="${WINDOWS_TEST_CACHE:-}" # keeps the installed Windows between runs
go="${GO:-go}"
work="$(mktemp -d)"
volume="gitea-mcp-windows-test-${work##*.}"
name=""

cleanup() {
  docker rm -fv "$name" >/dev/null 2>&1 || true
  docker volume rm "$volume" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

if [ "$(docker info -f '{{.Architecture}}')" != x86_64 ]; then
  echo "error: the Windows test VM needs an amd64 docker host" >&2
  exit 1
fi

echo "==> Building Windows test binaries"
shared="$work/shared"
mkdir -p "$work/oem" "$shared/bin" "$work/payload/src"
module="$("$go" list -m)"
packages="$(GOOS=windows "$go" list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)"
GOOS=windows GOARCH=amd64 "$go" test -c -ldflags='-s -w' -o "$shared/bin/" $packages
for package in $packages; do
  dir="${package#"$module"}"
  printf '%s.test.exe .%s\r\n' "${package##*/}" "${dir//\//\\}"
done > "$shared/tests.txt"
git ls-files -z --cached --others --exclude-standard | while IFS= read -rd '' file; do [ ! -e "$file" ] || printf '%s\0' "$file"; done |
  tar --null -T - -cf - | tar -xf - -C "$work/payload/src"
tar -cf "$shared/payload.tar" -C "$work/payload" src
sed 's/$/\r/' > "$shared/run.bat" <<'EOF'
@echo off
set log=\\host.lan\Data\test.log
echo ==^> Unpacking sources >> %log%
rd /s /q C:\tests 2> nul
md C:\tests
tar.exe -xf \\host.lan\Data\payload.tar -C C:\tests >> %log% 2>&1
set failed=0
for /f "tokens=1,2" %%a in (\\host.lan\Data\tests.txt) do (
  echo ==^> %%b >> %log%
  cd /d C:\tests\src\%%b 2>> %log% && \\host.lan\Data\bin\%%a -test.paniconexit0 -test.timeout=20m >> %log% 2>&1 || set failed=1
)
cd /d C:\
rd /s /q C:\tests
(echo %failed%)> \\host.lan\Data\exitcode
if not exist \\host.lan\Data\save (shutdown.exe /s /t 600 & exit /b)
:collected
if not exist \\host.lan\Data\collected (ping.exe -n 2 127.0.0.1 > nul & goto collected)
defrag.exe C: /L > nul
shutdown.exe /s /t 0
EOF
sed 's/$/\r/' > "$work/oem/install.bat" <<'EOF' # a policy turns off real-time scanning, AMSI blocks scripts that do it
@echo off
reg.exe add "HKLM\SOFTWARE\Policies\Microsoft\Windows Defender\Real-Time Protection" /v DisableRealtimeMonitoring /t REG_DWORD /d 1 /f > nul
(
  echo @echo off
  echo :wait
  echo if exist \\host.lan\Data\run.bat goto run
  echo ping.exe -n 3 127.0.0.1 ^> nul
  echo goto wait
  echo :run
  echo call \\host.lan\Data\run.bat
)> C:\run.bat
schtasks.exe /create /tn tests /sc onlogon /it /rl highest /tr C:\run.bat /f > nul
call C:\run.bat
EOF

kvm=Y cores=max
if ! docker run --rm --privileged --entrypoint sh "$image" -c 'test -w /dev/kvm'; then
  kvm=N cores=4 # more emulated vCPUs slow Windows down
  echo "==> The docker host has no usable /dev/kvm, Windows runs emulated and much slower"
fi

vm=(-e VERSION=2025 -e EDITION=core -e RAM_SIZE=8G -e DISK_SIZE=32G -e DISK_FMT=qcow2)
fingerprint="$(printf '%s\n' "$image" "${vm[*]}" | cat - "$work/oem/install.bat" | sha256sum | cut -d' ' -f1)" # what the installed disk depends on
docker volume create "$volume" >/dev/null
cached=N
if [ -n "$cache" ] && [ "$(cat "$cache/fingerprint" 2>/dev/null)" = "$fingerprint" ]; then
  cached=Y
  echo "==> Booting the cached Windows install"
else
  echo "==> Installing Windows"
  if [ -n "$cache" ]; then
    touch "$shared/save"
  fi
fi
deadline=$((SECONDS + timeout * 60))
boot() {
  name="gitea-mcp-windows-test-${work##*.}-$1"
  docker create --rm --name "$name" --privileged -v "$volume:/storage" "${vm[@]}" -e KVM="$kvm" -e CPU_CORES="$cores" -e DISK_CACHE=unsafe -e DISK_IO=threads \
    --entrypoint timeout "$image" "$(((deadline - SECONDS) / 60 + 10))m" /usr/bin/tini -s /run/entry.sh >/dev/null # the timeout stops the VM even when this script is killed before its cleanup runs
  if [ "$cached" = Y ]; then
    docker cp "$cache/storage/." "$name:/storage"
  else
    docker cp "$work/oem" "$name:/oem"
  fi
  docker cp "$shared" "$name:/shared"
  docker start "$name" >/dev/null
  docker logs -f "$name" &
  docker exec "$name" sh -c 'cd /shared && touch test.log && tail -f test.log' &
}

boots=1
boot "$boots"
until docker exec "$name" test -f /shared/exitcode 2>/dev/null; do
  if ((SECONDS >= deadline)) || [ "$(docker container inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" != true ]; then
    echo "Windows stopped or did not finish the tests within $timeout minutes" >&2
    exit 1
  fi
  if docker logs "$name" 2>&1 | grep -q 'No bootable option or device was found'; then # the firmware can give up on the install image
    if ((boots == 3)); then
      echo "Windows did not boot from its install image $boots times" >&2
      exit 1
    fi
    echo "==> Windows did not boot from its install image, starting a new VM"
    docker rm -fv "$name" >/dev/null
    docker volume rm "$volume" >/dev/null && docker volume create "$volume" >/dev/null
    boots=$((boots + 1))
    boot "$boots"
  fi
  if ((SECONDS % 300 < 10)); then
    echo "==> $((SECONDS / 60))m elapsed, VM disk $(docker exec "$name" du -sh /storage | cut -f1)"
  fi
  sleep 10
done
code="$(docker exec "$name" cat /shared/exitcode | tr -d '\r\n ')"
docker exec "$name" touch /shared/collected

if [ -f "$shared/save" ]; then
  echo "==> Saving the Windows install to $cache"
  until [ "$(docker container inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" != true ]; do
    sleep 2
  done
  docker run --rm -v "$volume:/storage" --entrypoint sh "$image" -c \
    'cd /storage && rm -f -- *.iso && qemu-img convert -O qcow2 data.qcow2 data.new && mv data.new data.qcow2'
  rm -rf "$cache" && mkdir -p "$cache"
  helper="$(docker create -v "$volume:/storage" --entrypoint true "$image")"
  docker cp "$helper:/storage" "$cache/storage"
  docker rm "$helper" >/dev/null
  echo "$fingerprint" > "$cache/fingerprint"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "save-cache=true" >> "$GITHUB_OUTPUT"
  fi
fi
exit "$code"
