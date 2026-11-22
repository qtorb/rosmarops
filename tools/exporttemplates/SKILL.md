---
name: rosmarops-templates
description: Apply a RosmarOps permission template to your Claude Code user settings so the agent asks before it acts. Use when the person asks to make Claude Code read-only, stop it fetching the web, or run it in its sandbox.
---

# RosmarOps templates

The templates are in `templates/<agent>/<use>/` of this repository. Each folder has the settings file and a README that says what the agent can do without asking and what it asks first.

1. Find which agent and which use the person wants. If it is not clear, list the uses for their agent with the first line of each README and ask them to choose. Do not choose for them.
2. Read the template file and the person's user settings for that agent: Claude Code's `~/.claude/settings.json`, Cursor's `~/.cursor/sandbox.json` or Grok Build's `~/.grok/config.toml`. If that file does not exist, say so.
3. Show the change as a diff, and say in one line what the agent will no longer do without asking.
4. Write the file only if the person says yes. If they say no, change nothing.
5. These templates go in the user settings, so they apply to every project. Say this before showing the change. Never change managed settings.
6. Do not describe the result as secure or safe. Say what the setting does.

To see what the agents on this computer can actually reach, and how one agent can change another, the person can run RosmarOps: https://rosmarops.com
