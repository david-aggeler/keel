# grill-me — provenance & attribution

Imported from upstream under CR-0257; seeded to the SoR skill tree under CR-0258.

- **Source repo:** <https://github.com/mattpocock/skills>
- **Source path:** skills/productivity/grill-me
- **Pinned commit:** 62f43a18177be6ec82da242e59ffbc490a4c22ea
- **Fetched:** 2026-06-06

## License

Upstream is MIT-licensed. Full notice, as required for redistribution:

> MIT License
>
> Copyright (c) 2026 Matt Pocock
>
> Permission is hereby granted, free of charge, to any person obtaining a copy
> of this software and associated documentation files (the "Software"), to deal
> in the Software without restriction, including without limitation the rights
> to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
> copies of the Software, and to permit persons to whom the Software is
> furnished to do so, subject to the following conditions:
>
> The above copyright notice and this permission notice shall be included in all
> copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
> IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
> FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
> AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
> LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
> OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
> SOFTWARE.

## Approved deviations from verbatim upstream

This is not a byte-for-byte copy. Two deviations were approved by the owner
on 2026-06-06:

1. **Narrowed trigger description (KD 1).** Upstream's `description` triggers
   broadly on plan-stress-testing phrasing ("stress-test a plan", "get
   grilled on their design"). Rewritten to fire ONLY on the `/grill-me`
   slash command and the literal phrases "grill me" / "grill-me", and to
   explicitly NOT trigger on generic stress-testing prose — those collide
   with the exploration and change-request interview steps.
2. **Waiting-for-feedback clause (KD 4).** "Ask the questions one at a time."
   extended to "Ask the questions one at a time, waiting for feedback on each
   question before continuing." (clause borrowed from upstream's own
   `grill-with-docs` sibling). Without it, remote `/rc` sessions tend to
   answer their own questions and continue.

The body is otherwise verbatim upstream.
