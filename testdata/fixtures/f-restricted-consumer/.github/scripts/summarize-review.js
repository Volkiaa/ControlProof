module.exports = async ({core, reviewPath}) => {
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
  await core.summary.addHeading('Review').addRaw(`PR #${pr}: ${state}`).write()
}
