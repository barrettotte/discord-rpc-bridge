# discord-rpc-bridge

A bridge to update Discord status when using Flatpak applications.

I recently switched over to Linux and noticed that Flatpak Steam and Flatpak Discord have trouble communicating due to the Flatpak app sandboxing. 
I also tried to symlink the Discord socket and tweak Flatpak permissions, but gave up.

So this is a tiny bridge to make it so Steam games show up in your Discord status as it would on Windows.


## Setup

## Limitations

- This only works for Steam games, but could potentially scan for other apps (KiCad, VSCode, etc.)
- I only tested with Rimworld, Balatro, and SHENZHEN IO on Fedora 43 Kinoite. But, it works for my use case so far.
