# hellobot

The smallest complete ConfigHub bot: one process, one Channel, two commands, and a log line
for every event the Channel subscribes to. It exists to be read and copied. The Go SDK's
`channel` package does the work; `main.go` is about 150 lines and most of them are comments.

A bot is a process you run that ConfigHub can ask to do things. It claims a
[Channel](https://docs.confighub.com/background/entities/channel/), and every
`cub command run` on that Channel reaches it as a run to execute and answer.
[Command bots](https://docs.confighub.com/developer/bots/) explains the model; this repository
is that page's quick start, runnable.

## Run it

You need `cub`, a ConfigHub organization you can create a Space in, and Go 1.25.

**1. An identity.** The bot authenticates as a service account with a key. The private half
stays with the bot:

```
cub serviceaccount create hellobot
cub serviceaccount key add hellobot --generate hellobot
```

**2. A Channel.** It lives in a Space and declares the commands the bot handles. The bot prints
them; the service account is granted Claim, the permission to be the Channel's reader:

```
go run . --print-commands > commands.json
cub channel create --space platform hellobot --commands commands.json --permission Claim:serviceaccount:hellobot
cub command list --space platform
```

**3. The bot:**

```
CONFIGHUB_URL=https://hub.confighub.com \
CONFIGHUB_AUTH_PRIVATE_KEY="$(cat ~/.confighub/keys/hellobot.jwk)" \
HELLOBOT_CHANNEL=platform/hellobot \
go run .
```

`cub channel get platform/hellobot` now shows the lease held and when the bot last polled.

**4. A command**, from another terminal, as yourself:

```
cub command run hello.say name=Ada
cub command run hello.say name=Ada shout=true
cub command run hello.wait seconds=5
cub commandrun list --space platform
```

Each run waits for its outcome and prints it; `hello.say` with an empty name is `Rejected`,
`hello.wait` with too many seconds is `Failed`, and the list is the record of all of them.

**5. Events**, optionally. Subscribe the Channel to Release publishes and the bot logs each one:

```
echo '[{"EventTypes": ["release.published"]}]' > subscriptions.json
cub channel update platform/hellobot --subscriptions subscriptions.json
cub release publish <some space>
```

The bot's account sees only events about Spaces it may view, so grant it View on a Space, or
ViewChildren on a Target, whose events it should receive.

## What to read in `main.go`

- `commands`: the declaration, the contract between whoever manages the Channel and this
  process. The listener refuses a Channel that declares anything else.
- The client: a key, an `Authenticator` that renews the session, nothing stored.
- `Handle`: a handler reads options with `String`, `Int`, `Bool`, `Has` or `Param`, and returns
  a result or an error. `channel.Reject` says "not mine to do"; any other error is `Failed`. A
  panic is recovered and reported. The context ends when the answer is overdue.
- `OnEvent`: events arrive over the same poll, in order, best effort. React, and reconcile from
  state elsewhere if it matters.
- `Run`: lease, verify, poll, dispatch, report, and on shutdown drain and release, so a
  replacement takes over without waiting for the lease to expire.

## Running it somewhere

`HELLOBOT_HTTP_PORT` serves `/healthz` and `/readyz`; readiness is holding the lease. One
process reads a Channel at a time, so run one replica, and let a rolling restart start the new
one before stopping the old. The `Dockerfile` builds a static image.

## Licence

MIT, like the SDK it is built on.
