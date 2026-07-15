# LatentTone

LatentTone is an open-source, self-hosted music server designed for automated audio discovery and continuous playback. The project shifts the self-hosted media paradigm from manual playlist management to algorithmic, seed-based stream generation. 
By leveraging mathematical vector representations of audio metadata and acoustic profiles, LatentTone uncovers latent relationships within local music libraries to deliver a continuous, contextually aware playback experience.

The architecture is built from the ground up to eliminate the legacy constraints of existing self-hosted media players
---

## Key Functional Pillars

### Algorithmic Stream Generation
* **Seed-Based Automation**: Initiates a continuous audio stream from a single track, artist, or genre input.
* **Vector-Space Mapping**: Embeds the music library into a multi-dimensional latent space to match tracks by structural, semantic, and acoustic affinity.
* **Real-Time Feedback Tuning**: Incorporates binary feedback (binary validation inputs) to dynamically adjust the active session's vector trajectory.

### Modern System Architecture
* **Decoupled Design**: Separates the data processing backend from the client interface via a strict API-first specification.
* **Container-Native**: Built for containerized deployment (Docker/Podman) with minimal external dependencies.

---
## Coming soon
