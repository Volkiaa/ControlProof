module.exports = async ({github, context, core, reviewPath}) => {
  const fs = require('fs')
  let review
  try {
    review = JSON.parse(fs.readFileSync(reviewPath, 'utf8'))
  } catch (error) {
    core.setFailed(`invalid review artifact: ${error.message}`)
    return
  }
  const {pr, state} = review
  if (!Number.isSafeInteger(pr) || pr <= 0) {
    core.setFailed('review artifact has an invalid pull request number')
    return
  }
  if (!['approved', 'changes_requested', 'commented'].includes(state)) {
    core.setFailed('review artifact has an invalid review state')
    return
  }
  const {data: pull} = await github.rest.pulls.get({
    owner: context.repo.owner,
    repo: context.repo.repo,
    pull_number: pr,
  })
  if (pull.head.sha !== context.payload.workflow_run.head_sha) {
    core.setFailed('review artifact does not match the triggering run')
    return
  }
  await github.rest.issues.addLabels({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: pr,
    labels: [`review:${state}`],
  })
}
