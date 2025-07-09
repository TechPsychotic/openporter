# OpenPorter: Zero-Client SSH Reverse Tunnel Gateway

![OpenPorter Logo](https://i.imgur.com/KV2N3yE.png) 

**OpenPorter is a self-hosted, secure, and extensible reverse tunnel gateway that allows you to expose local TCP services to the internet with a single SSH command. No client-client software is needed.**
![see this was made by gemini cli and wanted to see how that pans out. people do let met know how do you feel about this (as a project as well as how gemini pans out). also I am new to tunneling and all and hence would appreciate your comments as well as guidance on this]
---

## Table of Contents

- [Introduction](#introduction)
- [How It Works](#how-it-works)
- [Key Features](#key-features)
- [Deployment Guide](#deployment-guide)
- [Usage Guide](#usage-guide)
- [Admin & Maintenance](#admin--maintenance)
- [Security Overview](#security-overview)
- [Future Roadmap](#future-roadmap)
- [License](#license)

---

## Introduction

Do you need to share a local web server, give a friend access to your Minecraft server, or SSH into your Raspberry Pi from anywhere? OpenPorter makes this possible without installing tools like `ngrok` or configuring complex VPNs. It leverages the power of SSH, a tool already installed on virtually every developer machine, to create secure and reliable tunnels.

## How It Works

OpenPorter uses a secure, two-step process to establish tunnels. This is necessary to work around the limitations of SSH's `ForceCommand` and ensures that the server, not the client, controls tunnel configuration.

1.  **Request a Tunnel Configuration**: The user makes an initial SSH request to the server, asking for a new tunnel configuration. The `alias-manager` generates a unique public port and alias, saves it to the database (initially inactive), and sends the connection command back to the user.
    ```bash
    # Request a randomly-named tunnel
    ssh tunneluser@tunnel.net create
    
    # Or request a specific alias
    ssh tunneluser@tunnel.net alias:my-cool-app
    ```

2.  **Receive Connection Details**: The server responds with the details and the command to activate the tunnel.
    ```yaml
    ==== Tunnel Configuration Created ====

      Public URL:   red-bison.tunnel.net:32001
      Local Port:   32001

    To activate this tunnel, run the following command:

      ssh -R 32001:localhost:25565 tunneluser@tunnel.net

    This configuration will expire if not activated within 10 minutes.
    ```

3.  **Activate the Tunnel**: The user runs the provided `ssh -R` command. The `tunnel-handler` intercepts this, calls `alias-manager` to mark the tunnel as active, and the `tcp-mux` service begins proxying traffic from the public port to the user's local port. The SSH session must remain open to maintain the tunnel.

## Key Features

-   **Zero-Client**: Works with any standard SSH client. No agent installation required.
-   **Secure by Default**: Built on SSH public key authentication and user isolation.
-   **Full TCP Support**: Tunnels any TCP-based service (HTTP, SSH, RDP, databases, etc.).
-   **Self-Hosted**: You control your data, your domain, and your security.
-   **Lightweight**: Written in Go and Bash, with minimal resource consumption, perfect for Oracle's free tier.
-   **Persistent & Dynamic**: Tunnel configurations are stored in a SQLite database.
-   **Rate Limiting**: Prevents abuse by limiting tunnel creation attempts per user.
-   **User-Specific Tunnel Limits**: Restricts the number of active tunnels a single user can have.
-   **Automated Cleanup**: Inactive and unactivated tunnels are automatically removed.

## Deployment Guide

**Prerequisites**: A Linux server (Oracle Linux, Ubuntu, etc.), `git`, `golang`, and `sqlite` installed.

1.  **Create Users**: Create a locked-down `tunneluser` and a separate `admin` user for management.
    ```bash
    sudo useradd -m -s /usr/sbin/nologin tunneluser
    sudo useradd -m -s /bin/bash admin
    sudo passwd admin
    ```

2.  **Clone Repository**:
    ```bash
    sudo git clone https://github.com/your-username/openporter.git /opt/tunnel
    ```

3.  **Build Binaries**:
    ```bash
    sudo go build -o /opt/tunnel/bin/alias-manager /opt/tunnel/src/alias-manager/main.go
    sudo go build -o /opt/tunnel/bin/tcp-mux /opt/tunnel/src/tcp-mux/main.go
    ```

4.  **Initialize Database**:
    ```bash
    # WARNING: This will delete existing tunnel data if the file exists.
    sudo rm -f /opt/tunnel/var/db/tunnels.db
    sudo sqlite3 /opt/tunnel/var/db/tunnels.db < /opt/tunnel/var/db/schema.sql
    sudo chown -R tunneluser:tunneluser /opt/tunnel/var
    ```

5.  **Make Scripts Executable**:
    ```bash
    sudo chmod +x /opt/tunnel/libexec/tunnel-handler
    sudo chmod +x /opt/tunnel/libexec/get-ssh-fingerprint
    ```

6.  **Install and Start Services**:
    ```bash
    sudo cp /opt/tunnel/systemd/*.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now tcp-mux.service
    sudo systemctl enable --now tunnel-ssh.service
    ```

7.  **Configure SSH**:
    -   Add your users' public keys to `/home/tunneluser/.ssh/authorized_keys`.
    -   Add `Include /opt/tunnel/etc/sshd.conf.d/tunnel.conf` to your main `/etc/ssh/sshd_config`.
    -   Restart the SSH service: `sudo systemctl restart sshd`.

8.  **Configure DNS**:
    -   Create an **A record** for `tunnel.net` pointing to your server's IP.
    -   Create a wildcard **A record** for `*.tunnel.net` pointing to the same IP.

## Usage Guide

-   **Expose a Local Web Server on Port 3000**:
    1.  `ssh tunneluser@tunnel.net create`
    2.  You will receive a command like: `ssh -R 34567:localhost:3000 tunneluser@tunnel.net`
    3.  Run the command to activate the tunnel.
    4.  Access your web server at `random-alias.tunnel.net:34567`.

-   **SSH into a Home Machine (Port 22)**:
    1.  `ssh tunneluser@tunnel.net create`
    2.  You will receive a command like: `ssh -R 38022:localhost:22 tunneluser@tunnel.net`
    3.  Run the command to activate the tunnel.
    4.  From another machine, connect with `ssh user@random-alias.tunnel.net -p 38022`.

## Admin & Maintenance

-   **List Active Tunnels**:
    ```bash
    sudo sqlite3 /opt/tunnel/var/db/tunnels.db "SELECT * FROM tunnels WHERE active = 1;"
    ```
-   **Add a Reserved Alias**:
    ```bash
    echo "billing" | sudo tee -a /opt/tunnel/etc/tunnel/reserved_aliases
    ```
-   **View Logs**:
    ```bash
    tail -f /opt/tunnel/var/log/tunnel.log
    journalctl -u tcp-mux.service -f
    journalctl -u tunnel-ssh.service -f
    ```

## Security Overview

-   **User Isolation**: The `tunneluser` has no shell access due to `ForceCommand` and `nologin`.
-   **Authentication**: All access is through SSH public key authentication.
-   **Restricted Commands**: The `tunnel-handler` script carefully validates and sanitizes all user input.
-   **Rate Limiting**: Prevents rapid, successive tunnel creation attempts from a single user.
-   **User-Specific Tunnel Limits**: Configurable limits on the number of active tunnels per user to prevent resource exhaustion.
-   **Automated Cleanup**: Inactive tunnels (not activated within 10 minutes) and idle active tunnels (no traffic for 30 minutes) are automatically removed.
-   **Logging**: Detailed logging of tunnel creation, activation, and connection events for auditing and troubleshooting.

## Future Roadmap

-   [ ] **Web Dashboard**: A UI for viewing active tunnels and server status.
-   [ ] **Admin CLI**: A tool for the `admin` user to manage users and tunnels.
-   [ ] **TLS Termination**: Automatic HTTPS for web-based tunnels using Caddy or Let's Encrypt.
-   [ ] **Prometheus Metrics**: Exporting metrics for monitoring and alerting.
-   [ ] **Containerization**: Official Docker images and a `docker-compose` file for easy deployment.
-   [ ] **Advanced Token Management**: Allow users to generate and revoke API-like tokens.
-   [ ] **Bandwidth and Connection Limits**: Implement per-tunnel or per-user limits on bandwidth and concurrent connections.

## License

This project is licensed under the MIT License.
