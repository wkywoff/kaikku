# Security

## Reporting a vulnerability

Please don't open a public issue. Report it privately on GitHub:
**Security → Report a vulnerability** in this repository
([direct link](https://github.com/wkywoff/kaikku/security/advisories/new)).

Say what an attacker can do, how to reproduce it and which version or
commit you tested. You'll get an answer within a week. Once a fix is
released, the advisory is published with credit to you, unless you'd
rather stay anonymous.

## Supported versions

Only the latest release gets fixes. Update with
`git pull && docker compose pull && docker compose up -d`.

## Worth knowing

- kaikku is meant for you and the people you invite. Sign-up needs an
  invite, except for the very first account, which becomes the admin:
  create it right after starting a new instance.
- Keep `JWT_SECRET` and `MEDIA_SECRET` secret. Anyone with `JWT_SECRET`
  can sign in as any user.
- The database isn't exposed outside Docker's network. The site listens
  on `127.0.0.1` unless you set `HTTP_BIND`; put HTTPS in front before
  opening it to the internet.
