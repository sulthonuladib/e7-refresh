#!/bin/sh
# Install E7 Refresh for the current user, without root.
#
#   curl -fsSL https://raw.githubusercontent.com/sulthonuladib/e7-refresh/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --version v0.1.0
#   sh install.sh ./e7-refresh-0.1.0-linux-amd64.tar.gz
#   sh install.sh --uninstall
#
# Linux only. It downloads the release tarball for this machine and puts the
# app in ~/.local/e7-refresh.app, `e7-refresh` and `e7r` in ~/.local/bin, and
# an entry in the applications menu with this build's icon. Settings in
# ~/.config/e7-refresh and history in ~/.local/share/e7-refresh are never
# touched, not even by --uninstall.
set -eu

repo='sulthonuladib/e7-refresh'
name='e7-refresh'
app_name='E7 Refresh'
alt_command='e7r'

app_dir="$HOME/.local/$name.app"
bin_dir="$HOME/.local/bin"
data_home="${XDG_DATA_HOME:-$HOME/.local/share}"
desktop_file="$data_home/applications/$name.desktop"

usage() {
	cat <<EOF
Usage: sh install.sh [ARCHIVE | --version TAG | --uninstall]

  ARCHIVE        install a local $name-VERSION-linux-ARCH.tar.gz
  --version TAG  install TAG (for example v0.1.0) instead of the latest release
  --uninstall    remove the app from $app_dir, keeping settings and history

Without arguments it downloads the latest release for this machine.
EOF
}

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

say() {
	printf '%s\n' "$*"
}

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO- "$1"
	else
		fail "needs curl or wget"
	fi
}

download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	else
		wget -qO "$2" "$1"
	fi
}

update_databases() {
	for tool in update-desktop-database gtk-update-icon-cache; do
		command -v "$tool" >/dev/null 2>&1 || continue
		case "$tool" in
		update-desktop-database) "$tool" "$data_home/applications" >/dev/null 2>&1 || true ;;
		esac
	done
	if command -v xdg-desktop-menu >/dev/null 2>&1; then
		xdg-desktop-menu forceupdate >/dev/null 2>&1 || true
	fi
}

# restart_service picks up the new binary where the app was installed as a
# systemd user unit; an app that is not installed that way is left running.
restart_service() {
	command -v systemctl >/dev/null 2>&1 || return 0
	systemctl --user cat "$name.service" >/dev/null 2>&1 || return 0
	systemctl --user restart "$name.service" >/dev/null 2>&1 || true
	say "Restarted the $name service."
}

uninstall() {
	[ -d "$app_dir" ] || fail "$app_name is not installed in $app_dir"
	for link in "$bin_dir/$name" "$bin_dir/$alt_command"; do
		if [ -L "$link" ] && [ "$(readlink "$link")" = "$app_dir/$name" ]; then
			rm -f "$link"
		fi
	done
	if [ -f "$desktop_file" ] && grep -qF "$app_dir/" "$desktop_file"; then
		rm -f "$desktop_file"
	fi
	rm -rf "$app_dir"
	update_databases
	say "Uninstalled $app_name. Settings and history are still in place."
}

case "$(uname -s)" in
Linux) ;;
Darwin)
	say "$app_name installs from a .dmg on macOS: https://github.com/$repo/releases"
	exit 0
	;;
*)
	fail "$app_name's installer is for Linux; on Windows run the setup .exe from https://github.com/$repo/releases"
	;;
esac

[ "$(id -u)" != 0 ] || fail "run this as the user to install $app_name for, not as root"

tag=''
archive=''
action='install'
while [ $# -gt 0 ]; do
	case "$1" in
	-h | --help)
		usage
		exit 0
		;;
	--uninstall)
		action='uninstall'
		shift
		;;
	--version)
		[ $# -ge 2 ] || fail "--version needs a tag"
		tag="$2"
		shift 2
		;;
	-*)
		usage >&2
		exit 2
		;;
	*)
		archive="$1"
		shift
		;;
	esac
done

if [ "$action" = 'uninstall' ]; then
	uninstall
	exit 0
fi

case "$(uname -m)" in
x86_64 | amd64) arch='amd64' ;;
aarch64 | arm64) arch='arm64' ;;
*) fail "$app_name has no build for $(uname -m)" ;;
esac

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT HUP INT TERM

if [ -n "$archive" ]; then
	[ -f "$archive" ] || fail "no archive at $archive"
	say "Installing from $archive"
else
	if [ -n "$tag" ]; then
		release_url="https://api.github.com/repos/$repo/releases/tags/$tag"
	else
		release_url="https://api.github.com/repos/$repo/releases/latest"
	fi
	say "Looking up ${tag:-the latest release} of $app_name"
	listing="$(fetch "$release_url")" ||
		fail "cannot reach GitHub; download the archive from https://github.com/$repo/releases and pass it to this script"
	url=''
	for candidate in $(printf '%s' "$listing" |
		grep -o '"browser_download_url"[^,]*' |
		sed 's/.*"\(https[^"]*\)"/\1/'); do
		case "$candidate" in
		*"linux-$arch.tar.gz") url="$candidate" ;;
		esac
	done
	[ -n "$url" ] ||
		fail "no linux-$arch build in ${tag:-the latest release}; see https://github.com/$repo/releases"
	archive="$work/$(basename "$url")"
	say "Downloading $(basename "$url")"
	download "$url" "$archive" || fail "download failed: $url"
fi

mkdir -p "$work/app"
tar -xzf "$archive" -C "$work/app" || fail "$archive is not a .tar.gz archive"
[ -x "$work/app/$name" ] || fail "$archive is not $app_name: it holds no $name program"
if grep -Eq '^/|(^|/)\.\.(/|$)' <<EOF
$(tar -tzf "$archive")
EOF
then
	fail "$archive holds files outside the app"
fi

say "Installing $app_name in $app_dir"
rm -rf "$app_dir"
mv "$work/app" "$app_dir"

mkdir -p "$bin_dir"
ln -sf "$app_dir/$name" "$bin_dir/$name"
ln -sf "$app_dir/$name" "$bin_dir/$alt_command"

if [ -f "$app_dir/$name.desktop" ]; then
	mkdir -p "$data_home/applications"
	while IFS= read -r line || [ -n "$line" ]; do
		case "$line" in
		"Exec=$name") line="Exec=\"$app_dir/$name\"" ;;
		"Exec=$name "*) line="Exec=\"$app_dir/$name\"${line#"Exec=$name"}" ;;
		"Icon=$name") [ -f "$app_dir/$name.png" ] && line="Icon=$app_dir/$name.png" ;;
		esac
		printf '%s\n' "$line"
	done <"$app_dir/$name.desktop" >"$desktop_file"
fi
update_databases
restart_service

say "Installed $app_name: look for it in the applications menu, or run: $name"
case ":${PATH:-}:" in
*":$bin_dir:"*) ;;
*)
	say ""
	say "$bin_dir is not on PATH. Add it to run the commands from a shell:"
	say "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.profile"
	;;
esac
say ""
say "Next: $name settings          open the app's own window"
say "      $name install-service   start with the session, no window"
say "      $name install-bar       add the Omarchy bar widget"
