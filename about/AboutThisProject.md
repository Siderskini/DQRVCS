# About This Project

## Story

### How It Began
This project started with research into cryptography and blockchain out of personal interest.
At some point, I found myself reading the SPHINCS+ proposal which can be found [here](https://sphincs.org/data/sphincs+-submission-nist.zip).
I realized that there were very real techniques to use classical hardware to mitigate threats of quantum based attacks on cryptographic ledgers, and that there is publicly available, open source, MIT licensed code today that implements these techniques.
Naturally, this led me to question whether Bitcoin or other coins had implemented these techniques, and it turns out (surprisingly) that many of them do not.

I have no direct experience with building a cryptocurrency, wallet, transaction scheme, etc.
However, I do have experience using version control systems, which also maintain a ledger of historical changes.
Given this information, I did a Brave search on whether it would be technically possible to build a VCS that was enhanced through quantum resistance.
It turned out that not only was this technically possible, but there was active work towards something of that nature.
It also turns out that implementing a Quantum Resistant Ledger allows you to democratize the development of the shared repository through decentralization and consensus.
According to my Brave search, there was no commercially available git-like VCS built with quantum resistance in mind.
The obvious next step, now that I had identified a gap in the market, was to create something to fill that gap.

### The Process
The first thing to take care of was to formulate my thoughts about what was going to be built, why it was going to be built, and how it was going to be built. The result of this is the `Original README.md`, which is 100% hand-typed (took me about an hour maybe two). Obviously, I was wrong about many things, and the design wasn't fully fleshed out. However, it conveyed my intent pretty clearly, and I had a plan on how to proceed.

The next day, I began to follow the plan. You may notice that in the first step, I mention personally reimplementing Git in Go (haha). I ended up starting up a Codex session instead, and asking Codex to just follow the first step of the plan in the README.md. It decided to add a wrapper around Git, as the Git source code is over a quarter million lines of C++ and it just wouldn't make sense to reimplement the whole thing for the purpose of this project. At this point, I decided that I was no longer going to attempt to start with a hand-typed basic structure for Codex to follow.

Instead, I spent the entire day conversing with Codex instead, as it wrote thousands of lines of code, tests, and documentation. I wrote ZERO lines of Go or Rust code, and intervened pretty minimally to run some commands for Codex that required setup or permissions. The entirety of the Go code, Rust code, and Manual.md are fully written by Codex. At the end of the day, I had a functional CLI tool and a pretty nice-looking GUI to interact with that tool, both fully working on Ubuntu 24. Unfortunately, QRL was not as portable as I had originally thought, so the work on integrating the quantum ledger was deferred.

Today is the second day. I started the day off by asking Codex to rigorously test the GUI. It wrote a comprehensive set of tests, with structure and readability, and ended up fixing some of its previous code along the way. Within 10 minutes, the testing was written, run, and complete. Next, I ported the program over to MacOS (which actually required no changes whatsoever). So basically, less than an hour into the day, I had already accomplished half of what I wanted to. I considered testing on Windows, but figured I could defewr this for a bit, as I personally don't use Windows for development, and nobody else is using this code yet.

The next steps were to document this entire process, proofread the ~10k lines that Codex had written (removing any code that may disqualify this project from an Apache 2.0 License), and publish the project. I just wanted to create a checkpoint here because this project has come a long way and is almost ready for the publishing of the prototype.