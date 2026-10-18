# Contribute safely

Where circumvention is illegal, contributing can put you at risk. You can
contribute under a pseudonym: maintainers never ask for a real name, a photo, a
phone number or an identity document.

::: warning
These steps reduce the risk and cannot remove it. Everything you publish is
permanent and public. Decide before your first commit. Not contributing is a
valid choice.
:::

Security comes from doing, not just knowing. Do each step before your first
commit.

New to digital security? Start with the
[RaazNet Digital Security Checklist](https://checklist.raaznet.com/en/), in
English and Persian. It runs in your browser and keeps your answers on the page.
The steps below are specific to contributing here.

## 1. Create new accounts

Make a new email address and a new GitHub account, from the environment in step 2.
Reuse nothing from your real accounts.

| Do not | Do |
| --- | --- |
| `sarina-tehran-92` | `freebird-4f7k`, made up, nothing about you |
| Your usual email | A new address, made for this |
| Your usual photo | No photo |
| SMS or phone verification | An authenticator app |

Keep the recovery codes offline.

## 2. Use a separate environment

Use a separate device or browser profile, and never sign in to your real accounts
in it. [Tails](https://tails.net/) is built for this.

| Do not | Do |
| --- | --- |
| Open GitHub in the browser where you read your own email | Use Tails or a separate profile |

A separate Firefox profile is the minimum:

```sh
firefox -CreateProfile apranik-contribution
firefox -P apranik-contribution --no-remote
```

## 3. Configure git

Set the identity in the repository, not globally, and commit in UTC.

| Do not | Do |
| --- | --- |
| `git config --global` | `git config` inside the repository |
| `git commit` | `TZ=UTC git commit` |

```sh
git config user.name  "freebird-4f7k"
git config user.email "your-new-address@example.org"
TZ=UTC git commit
git log -1 --format=fuller   # check before you push
```

[GitHub's guide to the commit email address](https://docs.github.com/en/account-and-profile/setting-up-and-managing-your-personal-account-on-github/managing-email-preferences/setting-your-commit-email-address)
covers the rest.

## 4. Create new keys

Make an SSH key, and a GPG key if you sign commits, for this identity only.

| Do not | Do |
| --- | --- |
| Add the key your servers accept to GitHub | Make a new key for this identity |
| Keep the default comment, `sarina@laptop` | Set your own comment, `freebird-4f7k` |

```sh
ssh-keygen -t ed25519 -C "freebird-4f7k" -f ~/.ssh/apranik_contribution
git config core.sshCommand "ssh -i ~/.ssh/apranik_contribution -o IdentitiesOnly=yes"
```

::: details Why a key can identify you
GitHub publishes every public key of an account at
`https://github.com/<username>.keys`, and a server that accepts a key says so
without the private key. Crawlers match the two, so a key on your GitHub account
can tie it to your servers.
[Read the article](https://markpash.me/blog/fa-ssh-key-identity-discovery) (in
Persian).

Check both sides:

```sh
curl https://github.com/freebird-4f7k.keys
ssh -v -o IdentitiesOnly=yes -i ~/.ssh/some_key user@server.example 2>&1 | grep "Server accepts key"
```

The first shows what anyone can read about the account. If the second prints a
line, that server accepts the key, so the key must not be on your GitHub account.
:::

## 5. Keep the identity separate

Do not discuss your contributions with anyone who knows your real identity, or on
social media.

| Do not | Do |
| --- | --- |
| "I sent a fix to Apranik last night." | Say nothing about it |

## 6. Review what you share

Remove paths, usernames and IP addresses from logs, screenshots and files.

| Do not | Do |
| --- | --- |
| `/home/sarina/work/config.json` | `/home/user/work/config.json` |
| `dial tcp 198.51.100.7:443` | `dial tcp 203.0.113.1:443` |
| A screenshot showing your prompt | A cropped screenshot |

```sh
sed -E 's#/home/[^/ ]+#/home/user#g; s#[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+#203.0.113.1#g' log.txt > log-clean.txt
exiftool -all= screenshot.png   # removes the metadata from an image
```
