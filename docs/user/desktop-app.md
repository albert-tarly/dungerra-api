# Desktop App: Solo and LAN Play

The Dungerra desktop app runs the game on your own computer. You can play **alone without an internet connection**, **host** a game for others on your local network (the same home or office network), or **join** someone else's.

It is the same game as the online version, with the same weapons, enemies, shops and difficulties. What differs is where the world lives: on your computer, or on the computer of the player who hosts.

## Installing

The app is available for **Windows** (64-bit) and **macOS** (Apple Silicon and Intel).

- **Windows:** run the `Dungerra_…_x64-setup.exe` installer. No administrator rights are needed.
- **macOS:** open the `Dungerra_…_universal.dmg` file and drag **Dungerra** into **Applications**.

The app is not yet signed by a publisher, so your computer warns you the first time you open it:

- **Windows** shows "Windows protected your PC". Choose **More info**, then **Run anyway**.
- **macOS** says the app can't be checked for malicious software. Right-click (or Control-click) **Dungerra** in **Applications**, choose **Open**, then **Open** again. You can also allow it under **System Settings → Privacy & Security**. If macOS says the app "is damaged", open **Terminal** and run `xattr -dr com.apple.quarantine /Applications/Dungerra.app`.

## The launcher

The app opens on the launcher, which offers three choices:

| Choice | What it does |
| --- | --- |
| **Play solo** | Starts a game world on this computer that only you can reach. No internet needed. |
| **Host a LAN game** | Starts a game world on this computer that others on your network can join. |
| **Join a LAN game** | Connects to a game another player is hosting. |

After you choose, you continue as in the online game: sign in, then create or join a session in the session list (see [Getting Started](getting-started.md)).

To return to the launcher, use **Game → Back to Launcher** in the app's menu bar. This also stops a game you are hosting, so anyone connected to it is disconnected. Your world is saved first. **Game → Toggle Full Screen** switches full screen on and off.

## Accounts in the desktop app

The desktop app uses **local accounts** only: a username and password you choose (see [Signing in](getting-started.md#signing-in)). Google sign-in is not available.

- **Accounts belong to the computer that runs the game.** Your account for solo play exists only on your computer. To join a friend's game, create an account on their game the first time you join it.
- **You stay signed in.** Solo play and each host you join remember your sign-in for about 8 days.
- **Leaderboards are separate too.** Each computer that runs games keeps its own leaderboard, separate from the online leaderboard and from other players' computers.

## Playing solo

Choose **Play solo**. Your worlds, characters and leaderboard are saved on your computer, so you can stop at any time and continue later.

Solo play still runs in real time. The game does **not pause** when you open the game menu, so find a safe spot before you step away.

## Hosting a LAN game

1. Under **Host a LAN game**, keep the suggested port (**8080**) or enter another one from 1024 to 65535.
2. Choose **Start hosting**. The launcher shows the address others use to join, for example `192.168.1.5:8080`.
3. Share that address with the other players, then choose **Enter game** to play yourself.

While you host, your computer must stay on and the app must stay open. **Stop hosting**, or quitting the app, disconnects everyone and saves the world.

Players without the app can join too. They type the address into a web browser on the same network, for example `http://192.168.1.5:8080`.

### Allowing connections

The first time you host, your computer asks whether Dungerra may use the network. **Allow it**, or nobody can join:

- **Windows** asks whether to allow **dungerra-server** through Windows Defender Firewall. If your network is set to **Public**, nobody can join until you allow it. If you dismissed the question, open **Windows Security → Firewall & network protection → Allow an app through firewall** and allow **dungerra-server**.
- **macOS** asks whether Dungerra may find and connect to devices on your local network. To change your answer later, go to **System Settings → Privacy & Security → Local Network**.

## Joining a LAN game

1. Ask the host for their address, for example `192.168.1.5:8080`.
2. Under **Join a LAN game**, enter the address and choose **Join**. If you leave out the port, **8080** is used.
3. The launcher checks that a game is running there before it connects. If it isn't, it tells you what went wrong.

You play the host's world with the host's version of the game, so you never need to match versions. Your character, money and score are kept on the host's computer.

## If something goes wrong

| Problem | What to try |
| --- | --- |
| "Cannot reach …" when joining | Check the address. Make sure the host is still hosting, and that you are both on the same network: guest Wi-Fi often keeps devices apart. The host may also need to allow Dungerra through their firewall (see [Allowing connections](#allowing-connections)). |
| "Port … is already in use" when hosting | Another program uses that port. Enter a different port, for example 8081. |
| "The game server failed to start" | The launcher shows the reason. A detailed log is kept in `server.log` in the app's data folder (below). |

Your worlds, accounts and the server log are kept in the app's data folder:

- **Windows:** `%APPDATA%\io.github.albert-tarly.dungerra`
- **macOS:** `~/Library/Application Support/io.github.albert-tarly.dungerra`

Removing the app keeps this folder, so your worlds survive a reinstall. When you uninstall on Windows, you can tick the option to delete the app's data as well. Otherwise delete the folder yourself to remove all saved worlds and accounts.
