# Project Overview

This project is focused on managing the network state and configuration for the Speakerctld speaker system. It includes handling Wi-Fi connectivity, fallback mechanisms, and provisioning through a Go-based manager and Systemd integration.

## Key Components

- **Systemd**: Responsible for managing service dependencies, starting and stopping services, and handling targets for different network states.
- **Go-based Manager**: Handles the state machine, Wi-Fi configuration, provisioning, dynamic generation of runtime files, and decision-making for fallback scenarios.
- **Storage Layer**: Manages persistent storage of system state and Wi-Fi configuration.
- **Network Layer**: Manages Wi-Fi connectivity and monitors network status.
- **API Layer**: Provides REST endpoints for provisioning and network management.
- **UI Layer**: Provides the user interface for interacting with the network configuration and provisioning (captive portal and configuration pages).

## Architecture Overview
```PlainText
systemd
├── speakerctld.service
├── hostapd.service
├── dnsmasq.service
├── captive-portal.service
├── speakerctld-ap.target
└── wifi-client.target

Go-based Manager
├── cmd/speakerctld
├── internal/api
├── internal/state
├── internal/network
├── internal/systemd
├── internal/storage
├── internal/provisioning
├── internal/swupdate
├── internal/factoryreset
└── internal/ui
```

## Firmware Update

- `internal/swupdate` runs `swupdate -i <fichier.swu>` (jamais `swupdate-client`) et suit la progression via `swupdate-progress` (sortie parsée dans `progress.go`). Ces deux commandes sont l'exception à la règle « pas de commandes système » : elles n'ont pas d'interface DBus.
- `internal/api/firmware` : après succès (hors dry-run), le fichier `.swu` est supprimé puis le reboot est demandé via DBus logind (`systemd.Reboot()`, partagé avec `internal/factoryreset`). Le fichier est aussi supprimé en cas d'échec.
- UI : l'évènement WebSocket `firmware_update` avec `reboot: true` redirige vers `/rebooting` (page autonome, `internal/ui/settings/rebooting.html`) qui interroge `/api/system/ping` et revient sur `/` quand l'enceinte répond de nouveau.

# Implementation Plan

# contraintes

Toutes les interactions avec ces composants doivent passer par les interfaces DBus appropriées pour garantir la cohérence et la fiabilité du système.
Les appels directs aux commandes système ou aux fichiers de configuration ne sont pas autorisés par défaut.
Toutes les modifications de l'état du réseau ou des services doivent être effectuées via les méthodes exposées par DBus.
Les erreurs et les exceptions doivent être correctement gérées pour éviter des états incohérents du système.
Toutes les opérations critiques doivent inclure des mécanismes de reprise ou de rollback en cas d'échec.
Les journaux et les traces des interactions avec DBus doivent être conservés pour faciliter le débogage et l'audit du système.
Les modifications de l'état du réseau ou des services doivent être atomiques pour éviter des incohérences en cas de panne ou d'erreur.
Toutes les interactions avec DBus doivent être sécurisées et authentifiées pour prévenir les accès non autorisés.
Les permissions et les rôles doivent être correctement définis pour limiter l'accès aux fonctionnalités critiques du système.
Les modifications de la configuration réseau doivent être validées avant d'être appliquées pour éviter des états incohérents.
Les dépendances entre les services doivent être correctement gérées pour garantir un ordre d'exécution cohérent.
Les mécanismes de surveillance et de récupération doivent être en place pour détecter et corriger les défaillances du réseau ou des services.
Les mises à jour de la configuration ou du logiciel doivent être appliquées de manière sécurisée et contrôlée pour éviter toute interruption non planifiée du service.
