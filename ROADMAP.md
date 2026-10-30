# BurnBar roadmap

BurnBar currently supports Claude and Codex on the Windows taskbar. More AI tool
support is the next main direction. Priorities depend on community requests,
maintainer time, and whether a tool exposes a reliable way to read usage limits;
there are no promised release dates.

## Available now

- Claude and Codex quota windows, reset times, and usage history.
- A compact meter with provider rotation, alerts, and a details panel.
- Light/dark themes, display scaling, multiple taskbars, and adaptive placement.

## Next priorities

| Work | Status | How to help |
| --- | --- | --- |
| Evaluate additional AI tools and their usage sources | Accepting requests | [Request an AI tool](https://github.com/nooblk-98/burnbar/issues/new?template=02-ai-tool-request.yml) |
| Make adding providers simpler across fetching, settings, and rendering | Planned groundwork | [Discuss an implementation](https://github.com/nooblk-98/burnbar/issues/new?title=Provider%20integration%20groundwork&labels=enhancement) |
| Improve coverage of Windows taskbar layouts and scaling combinations | Ongoing | [Report a reproducible issue](https://github.com/nooblk-98/burnbar/issues/new?template=01-bug-report.yml) |

## AI tool request queue

Each proposed integration belongs in its own issue so feasibility, public
documentation, and test results stay together. Follow the
[open provider requests](https://github.com/nooblk-98/burnbar/issues?q=is%3Aissue%20is%3Aopen%20label%3Aprovider-request)
to see proposed tools and contribute information. No additional provider is
scheduled yet; a request is a proposal rather than a support announcement.

For a new request, include the tool's official website, the public plan name if
relevant, the limits you want to track, and public usage API or CLI documentation
if available. A provider can move toward implementation once a usable data source
and someone able to test it are identified. Please add details to an existing
request rather than opening a duplicate.

The [contribution guide](CONTRIBUTING.md) explains the code structure and checks
for contributors. Completed features are announced in
[GitHub Releases](https://github.com/nooblk-98/burnbar/releases).
