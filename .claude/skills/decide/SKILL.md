---
name: decide
description: "Frame a blocked decision for the owner: review the record, describe the user impact, record the outcome."
disable-model-invocation: true
---

# Prepare for the decision
- Review the underlaying record
- Challenge the assessment with data
- Make sure the topics are still relevant, all from code, SoR, and catalog perspective
- Convert the underlaying record details field to body type HTML and add an SVG per paragraph
- Mention the affected record(s)

# Show the options
- Describe the **user impact** of each option, so the owner can make a decision
- Show the question inline, without the annoying claude options dialog

# After the decision is done
- Update every record and its relatives 
- Perform a requirement/ac/user_need/architecture_description/interface_spec/design_decision/code consistency check.
- Move to an executable iteration. Try not to create new iterations
- Unblocked issues shall be concluded using conclude skill to an approved cr. 
