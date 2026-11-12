/** Capacity claims require observed people, connections, time and healthy work. */
export function assessCapacity(report) {
  const failures = [];
  const {config,joined,streamsHeld,resources=[]} = report;
  const need=(pass,text)=>{if(!pass)failures.push(text);};
  need(joined.players>=config.players,'insufficient players');
  need(joined.spectators>=config.spectators,'insufficient spectators');
  need(report.roomsCreated>=config.rooms && report.roundsCreated>=config.rooms,'insufficient active rooms');
  need(report.steadyDurationMs>=config.minutes*60000,'requested steady time not reached');
  need(report.steadyDurationMs>=60*60000,'less than 60 minutes observed');
  need(streamsHeld.players>=config.players && streamsHeld.spectators>=config.spectators,'audience connections not reached');
  need(streamsHeld.hosts>=config.rooms,'host connections not reached');
  need(report.prematureStreamCloses===0,'streams closed before intentional shutdown');
  need(report.totalSubmits>0,'no accepted gameplay writes');
  need(report.summaries.every(s=>s.errors===0),'request failures occurred');
  need(report.errors.length===0,'setup or runtime errors occurred');
  need(resources.length>=3 && resources.every(r=>r.serverRssKb>0 && r.goroutines>0 && Number.isInteger(r.activeConnections)),'missing runtime observations');
  const expectedConnections=config.players+config.spectators+config.rooms;
  need(resources.some(r=>r.phase==='all-streams-open' && r.activeConnections>=expectedConnections),'full connection count not observed by server');
  need(report.streamRelease?.activeAfter===0,'connections not released');
  const ops=report.summaries.filter(s=>s.total>0 && s.operation!=='streamConnect' && s.operation!=='submissionVisible');
  need(ops.length>0 && ops.every(s=>s.p95!==null && s.p95<=100 && s.p99!==null && s.p99<=250),'API latency exceeds target');
  const propagation=report.summaries.find(s=>s.operation==='submissionVisible');
  need(propagation?.total>0 && propagation.p95!==null && propagation.p95<=300,'submission visibility not measured within target');
  return {passed:failures.length===0,failures,note:'Local single-instance measurements only; not a production or native-network capacity promise.'};
}
