# Task Management CLI

## Background / Problem

Currently, I have a obsidian based Task management workflow. I have dedicated obisidian vault [agent-vault](~/agent-vault) where I put all my task artifacts. I have task artifacts template at [templates](~/agent-vault/templates) folder of my agent-vault. It defines `task.md`, `research.md` , `plan.md` and `review.md` file templates. I use an obsidian plugin that let me create new file from these templates. I also have some helper [commands](~/.config/opencode/commands/) for my opencode agent to find and work with the task artifacts.

When I want to start a new task for any of my projects, I follow this workflow:

1. In obsidian, I use the plugin to create a new `task.md` file from the template. The plugin ask me to provide task title. It creates relevant task artifacts folder and put `task.md` file there.
2. I define my task requirement in `task.md` file.
3. Then, I create a `.agent-task` file in my project repository root that point to the task artifacts directory.
4. I use `/research` command for the agent to identify the necessary things for the task. Agent writes it's finding in `research.md` file in the task folder. It follows the research file template I defined in the agent-vault.
5. Then, I use `/plan` command for the agent to write a detailed plan at `plan.md` file for the task. It breakdown the entire task into multiple PR sized subtasks. It uses plan file template for writing the plan.
6. I use `/next-subtask` command to signal agent to implement next subtask. Agent identify the first not-completed subtask and work on that.
7. Once agent finishes the subtask, I use `/review-subtask` command to tell another agent to review the subtask implementation. The reviewer agent review the code and write it's feedback into `review.md` file. It follows the review file template.
8. If there are some review feedback from the agent, I use `/address-review` command. Then, relevant agent will read the `review.md` file and address those findings.
9. Once a task is complete, agent updates the task status in `plan.md` file.
10. Any time, I can use `/task-status` command to see current status of the task.

This workflow has some friction but works. It depends on obsidian. I use Typora as my main markdown viewer. I would like to remove dependency on obsidian. Also, those opencode commands are not very token efficient. Agent has to read the task artifacts on task folder to understand a task status. Also, I often forget to update `.agent-task` with proper task path and it is not convenient to work on multiple tasks on separate branches.

## Goal

- Remove dependency on Obsidian.
- Avoid managing `.agent-task` manually.
- Make the opencode commands more token efficient.
- Use typora (or any other preferred markdown viewer) for reviewing the task artifacts.
- Provide a easy way for agent to discover the task status and task artifacts.
- Make the tasks git branch aware.
- The CLI should be supported in Linux and MacOS. Windows is out of scope for now.

## Proposal

I would like to crate a Go based CLI that will automate task management. I would like to call it `taskctl` inspired by Kubernetes `kubectl`. It may have following properties:

- Maintain a global index of tasks for different repositories, their branches, artifact path for the tasks.
- Maintain a local index on each of the task artifact folders that will record status of the task and subtasks.
- Provide commands for me to easily create new task, view status, open my markdown viewer at task artifact folder.
- Provide commands for agent to discover tasks, their artifacts, get/update task/subtask status.
- The task artifact file templates may be defined as go template file. For example `task.md.tmpl` , `research.md.tmpl`, `plan.md.tmpl` , `review.md.tmpl`, etc.

### Recommended Commands

#### taskctl init

Initial setup command. It will take input from user about the vault directory (where the indexes and task artifact will be), preferred application for viewing task artifacts. It may ask whether to initialize a git repository and add remote.

#### taskctl new "Task title"

Creates a new task. This command will take task title as argument. It will automatically resolve the project name and git branch. Then, it will create the relevant task artifacts folder (it may follow <project-name>/<git-branch> structure). Then, it will resolve the templates and create the sample artifact files.

#### taskctl context

Provide current task context. For example, is there task for current branch, task title, task status, sub-task count, how many of them completed vs total etc.

#### taskctl status

This will print detailed status of the current task. For example, it will print the summary like what `context` command did but print a formatted list of subtasks and their individual title, status etc.

#### taskctl path (task, plan, research, review)

This will return the path of respective `task.md`, `plan.md`, `research.md`, `review.md` files of current task. These are intended to be used by agent to discover the files easily.

#### taskctl subtask list

Returns list of subtasks, their id, status etc. of current task. This is intended to be used by agent.

#### taskctl subtask get

Return the subtask information of current task. It will return the current in-progress subtask status. If there is no subtask in-progress, it will return first pending subtask.

#### taskctl subtask update --id=<subtask-id> --status=<subtask-status>

This will update the status of a subtask. Intended to be used by agent. When all subtasks of a task are complete, the task itself should be marked complete automatically.

#### taskctl artifact view

Open the task artifacts folder in preferred application.

#### taskctl vault status

Shows git status of the task vault. If remote is configured, it should show whether it is behind or ahead of remote.

#### taskctl vault sync

It should commit current uncommitted changes. Rebase against remote (if remote has extra commits). Then, push to remote. If there is conflict, it will abort the rebase and ask user to fix the conflict.r
