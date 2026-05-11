[CmdletBinding()]
param(
  [string]$ApiBase = $env:DEMO_API_BASE,
  [int]$TimeoutSec = 120,
  [switch]$SkipScenarioGenerate
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

if ([string]::IsNullOrWhiteSpace($ApiBase)) {
  $ApiBase = 'http://localhost:8080/api/v1'
}

$ApiBase = $ApiBase.TrimEnd('/')
$ApiRoot = $ApiBase -replace '/api/v1$', ''
$script:Results = @()

function Invoke-DemoApi {
  param(
    [Parameter(Mandatory = $true)][string]$Method,
    [Parameter(Mandatory = $true)][string]$Path,
    [object]$Body = $null,
    [string]$Token = ''
  )

  $headers = @{ Accept = 'application/json' }
  if (-not [string]::IsNullOrWhiteSpace($Token)) {
    $headers.Authorization = "Bearer $Token"
  }

  $parameters = @{
    Uri        = "$ApiBase$Path"
    Method     = $Method
    Headers    = $headers
    TimeoutSec = $TimeoutSec
  }

  if ($null -ne $Body) {
    $json = $Body | ConvertTo-Json -Depth 40 -Compress
    $parameters.Body = [System.Text.Encoding]::UTF8.GetBytes($json)
    $parameters.ContentType = 'application/json; charset=utf-8'
  }

  $response = Invoke-RestMethod @parameters
  if ($null -eq $response.code -or $response.code -ge 400) {
    throw "API envelope error on $Method ${Path}: $($response.message)"
  }
  return $response.data
}

function Invoke-Step {
  param(
    [Parameter(Mandatory = $true)][string]$Name,
    [Parameter(Mandatory = $true)][scriptblock]$Action
  )

  Write-Host "==> $Name"
  try {
    $value = & $Action
    $script:Results += [pscustomobject]@{ Step = $Name; Result = 'PASS'; Detail = '' }
    Write-Host "    PASS" -ForegroundColor Green
    return $value
  } catch {
    $message = $_.Exception.Message
    $script:Results += [pscustomobject]@{ Step = $Name; Result = 'FAIL'; Detail = $message }
    Write-Host "    FAIL: $message" -ForegroundColor Red
    throw
  }
}

function Assert-Value {
  param(
    [bool]$Condition,
    [string]$Message
  )
  if (-not $Condition) {
    throw $Message
  }
}

$null = Invoke-Step -Name 'health check /healthz' -Action {
  $health = Invoke-RestMethod -Uri "$ApiRoot/healthz" -Method GET -TimeoutSec 15
  Assert-Value -Condition ($health.code -eq 200 -and $health.data.status -eq 'ok') -Message 'healthz did not return ok'
}

$aiStatus = Invoke-Step -Name 'read AI status' -Action {
  Invoke-DemoApi -Method GET -Path '/system/ai'
}
Write-Host "    AI provider=$($aiStatus.provider), model=$($aiStatus.model), fallback=$($aiStatus.fallback)"

$student = Invoke-Step -Name 'login student demo' -Action {
  Invoke-DemoApi -Method POST -Path '/auth/login' -Body @{ identifier = 'demo'; password = 'demo123' }
}
Assert-Value -Condition ($student.user.role -eq 'student') -Message 'demo role is not student'

$instructor = Invoke-Step -Name 'login instructor' -Action {
  Invoke-DemoApi -Method POST -Path '/auth/login' -Body @{ identifier = 'instructor'; password = 'instructor123' }
}
Assert-Value -Condition ($instructor.user.role -eq 'instructor') -Message 'instructor role is not instructor'

$admin = Invoke-Step -Name 'login admin' -Action {
  Invoke-DemoApi -Method POST -Path '/auth/login' -Body @{ identifier = 'admin'; password = 'admin123' }
}
Assert-Value -Condition ($admin.user.role -eq 'admin') -Message 'admin role is not admin'

$question = $null
if (-not $SkipScenarioGenerate) {
  $createdJob = Invoke-Step -Name 'create async scenario generation job' -Action {
    Invoke-DemoApi -Method POST -Path '/scenarios/generate/jobs' -Token $student.access_token -Body @{
      domain        = 'database'
      difficulty    = 'L2'
      scenario_type = 'troubleshooting'
      tags          = @('demo-smoke', 'validation')
    }
  }
  Assert-Value -Condition (-not [string]::IsNullOrWhiteSpace($createdJob.job.id)) -Message 'generation job id is empty'

  $deadline = (Get-Date).AddSeconds($TimeoutSec)
  $generated = $null
  while ((Get-Date) -lt $deadline) {
    $generated = Invoke-Step -Name 'poll async scenario generation job' -Action {
      Invoke-DemoApi -Method GET -Path "/ai/jobs/$($createdJob.job.id)" -Token $student.access_token
    }
    if ($generated.job.status -eq 'completed') {
      break
    }
    if ($generated.job.status -eq 'failed') {
      throw "generation job failed: $($generated.job.error_message)"
    }
    Start-Sleep -Seconds 2
  }
  Assert-Value -Condition ($null -ne $generated -and $generated.job.status -eq 'completed') -Message 'generation job did not complete'
  Assert-Value -Condition (-not [string]::IsNullOrWhiteSpace($generated.job.result_question_id)) -Message 'generated question_id is empty'
  $question = $generated.question
  Write-Host "    generated=$($question.title), provider=$($generated.job.provider), fallback=$($generated.job.fallback_used)"
}

if ($null -eq $question) {
  $scenarioList = Invoke-Step -Name 'read seed scenario question' -Action {
    Invoke-DemoApi -Method GET -Path '/scenarios?domain=database' -Token $student.access_token
  }
  $question = @($scenarioList.list)[0]
  Assert-Value -Condition ($null -ne $question) -Message 'no scenario question available'
}

$scenarioSession = Invoke-Step -Name 'create scenario session' -Action {
  Invoke-DemoApi -Method POST -Path "/scenarios/$($question.id)/sessions" -Token $student.access_token
}
Assert-Value -Condition (-not [string]::IsNullOrWhiteSpace($scenarioSession.session_id)) -Message 'scenario session_id is empty'

$null = Invoke-Step -Name 'send scenario message' -Action {
  Invoke-DemoApi -Method POST -Path "/scenarios/sessions/$($scenarioSession.session_id)/messages" -Token $student.access_token -Body @{
    content = 'Please provide the current symptoms, logs, and metric clues first.'
  }
}

$null = Invoke-Step -Name 'submit scenario answer' -Action {
  Invoke-DemoApi -Method POST -Path "/scenarios/sessions/$($scenarioSession.session_id)/answer" -Token $student.access_token -Body @{
    answer = 'Use symptoms, logs, and key metrics to locate the root cause, roll back the risky change, and verify recovery.'
  }
}

$scenarioReview = Invoke-Step -Name 'read scenario review' -Action {
  Invoke-DemoApi -Method GET -Path "/scenarios/sessions/$($scenarioSession.session_id)/review" -Token $student.access_token
}
Assert-Value -Condition ($scenarioReview.standard_steps.Count -gt 0) -Message 'scenario review has no standard steps'

$interview = Invoke-Step -Name 'create interview session' -Action {
  Invoke-DemoApi -Method POST -Path '/interviews/sessions' -Token $student.access_token -Body @{
    domain        = 'database'
    difficulty    = 'L3'
    question_type = 'scenario_analysis'
  }
}
Assert-Value -Condition (-not [string]::IsNullOrWhiteSpace($interview.session_id)) -Message 'interview session_id is empty'

$interviewResult = Invoke-Step -Name 'submit interview answer' -Action {
  Invoke-DemoApi -Method POST -Path "/interviews/sessions/$($interview.session_id)/submit" -Token $student.access_token -Body @{
    content = 'I would confirm impact, error rate, slow queries, and connection pool metrics, then isolate recent changes and provide rollback, throttling, and verification steps.'
    type    = 'text'
  }
}

$roundGuard = 0
while ($interviewResult.session_status -ne 'final_evaluated' -and $roundGuard -lt 3) {
  $roundGuard++
  $interviewResult = Invoke-Step -Name "answer interview follow-up $roundGuard" -Action {
    Invoke-DemoApi -Method POST -Path "/interviews/sessions/$($interview.session_id)/followup/answer" -Token $student.access_token -Body @{
      content = 'I would add measurable indicators, verification commands, rollback windows, and retrospective improvements.'
      type    = 'text'
    }
  }
}
Assert-Value -Condition ($interviewResult.session_status -eq 'final_evaluated') -Message 'interview did not reach final_evaluated'

$report = Invoke-Step -Name 'read interview report' -Action {
  Invoke-DemoApi -Method GET -Path "/interviews/sessions/$($interview.session_id)/report" -Token $student.access_token
}
Assert-Value -Condition ($report.radar_data.Count -gt 0) -Message 'interview report has no radar data'

$stamp = Get-Date -Format 'yyyyMMddHHmmss'
$post = Invoke-Step -Name 'student creates UGC preview' -Action {
  Invoke-DemoApi -Method POST -Path '/community/posts' -Token $student.access_token -Body @{
    title       = "Demo acceptance case $stamp"
    raw_content = 'After a cache key rule release, hit rate dropped and database read traffic increased. Convert this into a troubleshooting scenario.'
    domain      = 'database'
    tags        = @('demo-smoke', 'ugc')
  }
}
Assert-Value -Condition ($post.status -eq 'pending_review') -Message 'new UGC post is not pending_review'

$approved = Invoke-Step -Name 'instructor approves UGC' -Action {
  Invoke-DemoApi -Method POST -Path "/community/posts/$($post.id)/instructor-review" -Token $instructor.access_token -Body @{
    decision = 'approve'
    note     = 'Content is complete enough for final review.'
  }
}
Assert-Value -Condition ($approved.status -eq 'instructor_approved') -Message 'UGC post was not instructor_approved'

$published = Invoke-Step -Name 'admin publishes UGC as scenario' -Action {
  Invoke-DemoApi -Method POST -Path "/community/posts/$($post.id)/final-review" -Token $admin.access_token -Body @{
    decision = 'publish'
    note     = 'Publish as a demo scenario.'
  }
}
Assert-Value -Condition ($published.post.status -eq 'published') -Message 'UGC post was not published'
Assert-Value -Condition (-not [string]::IsNullOrWhiteSpace($published.post.converted_question_id)) -Message 'converted_question_id is empty'

$studentScenarioView = Invoke-Step -Name 'student reads sanitized converted scenario' -Action {
  Invoke-DemoApi -Method GET -Path "/scenarios/$($published.post.converted_question_id)" -Token $student.access_token
}
Assert-Value -Condition ($studentScenarioView.is_sanitized -eq $true) -Message 'student scenario view is not sanitized'
Assert-Value -Condition ([string]::IsNullOrWhiteSpace($studentScenarioView.content.root_cause)) -Message 'student scenario view leaked root_cause'

$null = Invoke-Step -Name 'admin reads user list' -Action {
  $users = Invoke-DemoApi -Method GET -Path '/admin/users' -Token $admin.access_token
  Assert-Value -Condition (@($users.list).Count -ge 3) -Message 'admin users list has fewer than 3 users'
}

Write-Host ''
Write-Host 'Demo acceptance summary:' -ForegroundColor Cyan
$script:Results | Format-Table -AutoSize
Write-Host 'All demo acceptance checks passed.' -ForegroundColor Green
