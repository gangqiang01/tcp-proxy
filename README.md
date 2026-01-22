# How a TCP Proxy Server Works: Connecting Two Endpoints

## Overview
A TCP proxy server acts as an intermediary or relay between two network endpoints: a **client** and a **backend server**. Its primary function is to forward network traffic seamlessly between them. This makes the proxy appear as the final server to the client, and conversely, appear as the client to the backend server.
## how run
make

## Step-by-Step Breakdown of the Connection Process

### 1. Listen for Connections
The proxy server starts by opening a network **socket** and binding it to a specific IP address and port. It then listens for incoming connection requests from clients.

### 2. Accept Client Connection
When a client (e.g., a web browser, an application) initiates a connection intended for the backend server, it actually connects to the proxy server's listening address. The proxy **accepts** this incoming connection, establishing a full TCP connection with the client (referred to as **Socket A**).

### 3. Connect to Backend Server
After accepting the client connection (and often after reading the initial request to determine the target), the proxy server initiates a **new, separate TCP connection** to the intended backend server (e.g., a web server, database). This is referred to as **Socket B**.

### 4. Bidirectional Data Forwarding (The Core)
Once both connections are established, the proxy enters its main loop, performing **full-duplex data relaying**:
- It continuously **reads** data from the client on Socket A and **writes** that data to the backend server on Socket B.
- Simultaneously, it **reads** data from the backend server on Socket B and **writes** it back to the client on Socket A.

### 5. Connection Termination
When either endpoint closes its connection, the proxy detects this, closes the corresponding connection to the other endpoint, and cleans up the resources for both sockets. This ensures a complete teardown of the relayed session.

## Key Characteristics of a TCP Proxy

| Characteristic | Description |
| :--- | :--- |
| **Protocol Agnostic** | Operates at the transport layer (TCP), so it can forward any application-layer protocol (HTTP, SSH, SMTP, raw data) that uses TCP. |
| **Transparent Relay** | Ideally forwards data byte-for-byte without modification, making the connection appear direct to both ends. |
| **Network Abstraction** | Can provide benefits like hiding the backend server's real IP, enabling access control, logging traffic, or connecting networks that cannot route directly. |

## Summary
In essence, a TCP proxy server is a dedicated middleman that creates two independent TCP connections and diligently copies data between them, effectively "gluing" the client and server together through itself.
