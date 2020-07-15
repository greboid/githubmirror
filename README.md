# Github Mirror

CLI utility to mirror all github repositories to a local directory.  All repositories will be clone, 
or updated as required, they will be stored under the path specified sorted by username.

### Access token

Running this will require a [github personal access token](https://github.com/settings/tokens), 
this will require the `repo` or `public_repo` scope to be  able to list the repositories to clone.

## Docker usage

Available as a docker image, will access either CLI arguments or environmental variables for configuration.

```
version: '3.7'

services:
  webhooked:
    image: greboid/githubmirror
    environment:
      AUTHTOKEN: <access token>
      CHECKOUTPATH: /repos
      DURATION: 1h
    volumes:
      - <local path>:/repos
    restart: always
```

## Basic CLI Usage

This can also be installed and run directly:

```
go install github.com/greboid/githubmirror
```
    
```
  githubmirror \
    --authtoken [authToken] \
    --checkoutpath [root checkout path]  \
    --duration [repeat every X duration]
```