/**
 * Pure Voximplant softphone scenario:
 * - inbound Vox DID → mapped agent (Web SDK)
 * - outbound city → callPSTN with that agent's CLI
 * - user ↔ user
 * - запись разговора → CRM (прослушивание в карточке лида)
 *
 * No Exolve SIP. Caller ID must be a number attached to this Vox application.
 * agent17 принимает на 79014171300 и 79014691012, звонит только с 79014691012.
 * agent18 принимает на 79014171406 и 79014682857, звонит только с 79014682857.
 * agent30 принимает на 79014172349 и 79014683098, звонит только с 79014683098.
 * agent31 принимает на 79014172447 и 79014689793, звонит только с 79014689793.
 * agent62 принимает на 79014175249 и 79014690533, звонит только с 79014690533.
 * agent64 принимает на 79014175476 и 79014681427, звонит только с 79014681427.
 * agent65 принимает на 79014175563 и 79014691301, звонит только с 79014691301.
 * agent66 принимает на 79014175739 и 79014681602, звонит только с 79014681602.
 * agent67 принимает на 79014175784, 79014681280 и 79014684575, звонит только с 79014684575.
 * agent68 принимает на 79014175799, 79014681493, 79014690172 и 79014691431, звонит только с 79014691431.
 * agent69 принимает на 79014175864 и 79014682046, звонит только с 79014682046.
 * agent70 принимает на 79014175941 и 79014690973, звонит только с 79014690973.
 * agent74 принимает на 79014176205, 79014684715 и 79014692313, звонит только с 79014692313.
 * agent75 принимает на 79014176337, 79014684293 и 79014691687, звонит только с 79014691687.
 * agent76 принимает на 79014176500 и 79014684273, звонит только с 79014684273.
 * agent77 принимает на 79014176561, 79014684266 и 79014685386, звонит только с 79014685386.
 * agent85 принимает на 79014177314, 79014684409, 79014688682 и 79014691094, звонит только с 79014691094.
 * agent86 принимает на 79014177331 и 79014688762, звонит только с 79014688762.
 * agent109 принимает на 79014680832, 79014683947 и 79014688713, звонит только с 79014688713.
 * 79014690854 — общий вход: звонит всем агентам из DID_TO_USER, соединяется первый, кто ответил.
 *
 * Create Users agent1…agent101 in Vox (same short names), then bind each
 * username+password in CRM → Employees → telephony.
 *
 * Запись: в приложении → Секреты (или старый Application Config) ключ
 *   crm_call_webhook_secret = значение CALL_WEBHOOK_SECRET из .env CRM
 * URL вебхука: https://kr-flow.ru/webhooks/telephony/voximplant/{secret}
 */

require(Modules.Recorder);

/**
 * DID (digits) → short Vox username.
 * agent1 keeps the original working number.
 */
var DID_TO_USER = {
  79014173381: 'agent1',
  79014170791: 'agent2',
  79014170908: 'agent3',
  79014170914: 'agent4',
  79014680467: 'agent5',
  79014170956: 'agent6',
  79014171096: 'agent7',
  79014171128: 'agent8',
  79014680442: 'agent9',
  79014171215: 'agent10',
  79014171230: 'agent11',
  79014171243: 'agent12',
  79014171250: 'agent13',
  79014171261: 'agent14',
  79014171263: 'agent15',
  79014171289: 'agent16',
  79014171300: 'agent17',
  79014691012: 'agent17',
  79014171406: 'agent18',
  79014682857: 'agent18',
  79014171519: 'agent19',
  79014171597: 'agent20',
  79014171620: 'agent21',
  79014171823: 'agent22',
  79014171830: 'agent23',
  79014171875: 'agent24',
  79014171889: 'agent25',
  79014171896: 'agent26',
  79014172249: 'agent27',
  79014172254: 'agent28',
  79014172283: 'agent29',
  79014172349: 'agent30',
  79014683098: 'agent30',
  79014172447: 'agent31',
  79014689793: 'agent31',
  79014172509: 'agent32',
  79014172546: 'agent33',
  79014172593: 'agent34',
  79014172645: 'agent35',
  79014172812: 'agent36',
  79014172855: 'agent37',
  79014172877: 'agent38',
  79014173081: 'agent39',
  79014173089: 'agent40',
  79014173234: 'agent41',
  79014173277: 'agent42',
  79014173303: 'agent43',
  79014173418: 'agent44',
  79014173445: 'agent45',
  79014173484: 'agent46',
  79014173490: 'agent47',
  79014173496: 'agent48',
  79014173553: 'agent49',
  79014173557: 'agent50',
  79014173582: 'agent51',
  79014173665: 'agent52',
  79014173962: 'agent53',
  79014174069: 'agent54',
  79014174078: 'agent55',
  79014174278: 'agent56',
  79014174374: 'agent57',
  79014174501: 'agent58',
  79014174774: 'agent59',
  79014174794: 'agent60',
  79014175239: 'agent61',
  79014175249: 'agent62',
  79014690533: 'agent62',
  79014175473: 'agent63',
  79014175476: 'agent64',
  79014681427: 'agent64',
  79014175563: 'agent65',
  79014691301: 'agent65',
  79014175739: 'agent66',
  79014681602: 'agent66',
  79014175784: 'agent67',
  79014681280: 'agent67',
  79014684575: 'agent67',
  79014175799: 'agent68',
  79014681493: 'agent68',
  79014690172: 'agent68',
  79014691431: 'agent68',
  79014175864: 'agent69',
  79014682046: 'agent69',
  79014175941: 'agent70',
  79014690973: 'agent70',
  79014175996: 'agent71',
  79014176016: 'agent72',
  79014176100: 'agent73',
  79014176205: 'agent74',
  79014684715: 'agent74',
  79014692313: 'agent74',
  79014176337: 'agent75',
  79014684293: 'agent75',
  79014691687: 'agent75',
  79014176500: 'agent76',
  79014684273: 'agent76',
  79014176561: 'agent77',
  79014684266: 'agent77',
  79014685386: 'agent77',
  79014176617: 'agent78',
  79014680683: 'agent79',
  79014176997: 'agent80',
  79014177032: 'agent81',
  79014177052: 'agent82',
  79014177204: 'agent83',
  79014177232: 'agent84',
  79014177314: 'agent85',
  79014684409: 'agent85',
  79014688682: 'agent85',
  79014691094: 'agent85',
  79014177331: 'agent86',
  79014688762: 'agent86',
  79014177346: 'agent87',
  79014177689: 'agent88',
  79014681221: 'agent89',
  79014177917: 'agent90',
  79014177948: 'agent91',
  79014178029: 'agent92',
  79014178049: 'agent93',
  79014178295: 'agent94',
  79014178393: 'agent95',
  79014178407: 'agent96',
  79014178475: 'agent97',
  79014178572: 'agent98',
  79014178609: 'agent99',
  79014178672: 'agent100',
  79014178820: 'agent101',
  79014178821: 'agent102',
  79014179064: 'agent103',
  79014179365: 'agent104',
  79014179391: 'agent105',
  79014680175: 'agent108',
  79014680832: 'agent109',
  79014683947: 'agent109',
  79014688713: 'agent109',
  79014683458: 'agent110',
  79014684415: 'agent111',
  79014687148: 'agent113',
  79014690676: 'agent115',
};

/** Общий вход лид-генератора. Не привязан к одному агенту. */
var SHARED_INBOUND_DID = '79014690854';

/** Fallback CLI if user has no mapped DID. */
var DEFAULT_OUTBOUND_CALLER_ID = '+79014173381';

/** Если у агента два номера, исходящий берём отсюда, а не первый из DID_TO_USER. */
var OUTBOUND_CLI = {
  agent17: '79014691012',
  agent18: '79014682857',
  agent30: '79014683098',
  agent31: '79014689793',
  agent62: '79014690533',
  agent64: '79014681427',
  agent66: '79014681602',
  agent67: '79014684575',
  agent65: '79014691301',
  agent68: '79014691431',
  agent69: '79014682046',
  agent70: '79014690973',
  agent74: '79014692313',
  agent75: '79014691687',
  agent76: '79014684273',
  agent77: '79014685386',
  agent85: '79014691094',
  agent86: '79014688762',
  agent109: '79014688713',
};

/** Prod CRM host for recording webhooks. */
var CRM_PUBLIC_BASE = 'https://kr-flow.ru';

function recordingWebhookSecret() {
  var name = 'crm_call_webhook_secret';
  try {
    if (typeof VoxEngine !== 'undefined' && VoxEngine.getSecretValue) {
      var fromSecrets = VoxEngine.getSecretValue(name);
      if (fromSecrets != null && String(fromSecrets).trim()) {
        return String(fromSecrets).trim();
      }
    }
  } catch (e) {
    /* no secrets API */
  }
  try {
    if (typeof Config !== 'undefined' && Config.get) {
      var fromConfig = Config.get(name);
      if (fromConfig != null && String(fromConfig).trim()) {
        return String(fromConfig).trim();
      }
    }
  } catch (e2) {
    /* no config */
  }
  return '';
}

function recordingWebhookURL() {
  var secret = recordingWebhookSecret();
  if (!secret) return '';
  return CRM_PUBLIC_BASE + '/webhooks/telephony/voximplant/' + secret;
}

/**
 * Пишем обе ноги в один recorder только после ответа (Connected у набранной ноги).
 * Гудки в файл не входят. Если createRecorder недоступен — call.record().
 */
function enableCallRecording(callA, callB, startNow) {
  var hook = recordingWebhookURL();
  if (!hook) {
    Logger.write(
      'Recording webhook not configured (Secrets/Config crm_call_webhook_secret)'
    );
    return;
  }
  if (!callB) return;

  var url = '';
  var posted = false;
  var started = false;
  function postOnce(reason) {
    if (posted || !url) return;
    posted = true;
    Logger.write('Recording post (' + reason + '): ' + url);
    postRecordingToCRM(hook, url, callA, callB);
  }

  function begin() {
    if (started) return;
    started = true;
    Logger.write('Answered — start recording');

    if (typeof VoxEngine.createRecorder === 'function') {
      var recorder;
      try {
        recorder = VoxEngine.createRecorder({ hd_audio: true });
      } catch (e) {
        Logger.write('createRecorder failed: ' + e);
        recorder = null;
      }
      if (recorder) {
        Logger.write('Using createRecorder');
        recorder.addEventListener(RecorderEvents.Started, function (ev) {
          url = String((ev && ev.url) || '');
          Logger.write('Recording started: ' + url);
        });
        recorder.addEventListener(RecorderEvents.Stopped, function () {
          Logger.write('Recording stopped: ' + url);
          postOnce('stopped');
        });
        function send(call) {
          if (!call) return;
          try {
            call.sendMediaTo(recorder);
          } catch (err) {
            Logger.write('sendMediaTo: ' + err);
          }
        }
        send(callA);
        send(callB);
        return;
      }
    }

    Logger.write(
      'Fallback call.record (createRecorder typeof=' +
        typeof VoxEngine.createRecorder +
        ')'
    );
    function arm(call, label) {
      if (!call || typeof call.record !== 'function') {
        Logger.write('call.record missing on ' + label);
        return;
      }
      call.addEventListener(CallEvents.RecordStarted, function (ev) {
        var u = String((ev && ev.url) || '');
        if (u) {
          url = u;
          Logger.write('Recording started (' + label + '): ' + url);
        }
      });
      call.addEventListener(CallEvents.RecordStopped, function () {
        postOnce('record-stopped-' + label);
      });
      try {
        call.record({ hd_audio: true });
        Logger.write('call.record armed on ' + label);
      } catch (err) {
        Logger.write('call.record failed on ' + label + ': ' + err);
      }
    }
    arm(callA, 'A');
    arm(callB, 'B');
  }

  callB.addEventListener(CallEvents.Connected, begin);
  if (startNow) begin();
  function watchEnd(call) {
    if (!call) return;
    call.addEventListener(CallEvents.Disconnected, function () {
      postOnce('disconnected');
    });
  }
  watchEnd(callA);
  watchEnd(callB);
}

function postRecordingToCRM(hook, url, callA, callB) {
  if (!hook || !url) return;
  var from = '';
  var to = '';
  try {
    from = String(
      (callA && callA.callerid && callA.callerid()) || (callA && callA.number) || ''
    );
  } catch (e) {
    /* ignore */
  }
  try {
    to = String(
      (callB && callB.number) || (callB && callB.callerid && callB.callerid()) || ''
    );
  } catch (e) {
    /* ignore */
  }
  var body = JSON.stringify({
    event: 'recording',
    provider: 'voximplant',
    recording_url: url,
    from: from,
    to: to,
  });
  Net.httpRequestAsync(hook, {
    headers: ['Content-Type: application/json'],
    method: 'POST',
    postData: body,
  })
    .then(function () {
      Logger.write('Recording posted to CRM');
    })
    .catch(function (err) {
      Logger.write('Recording post failed: ' + err);
    });
}

function onCallAlerting(e) {
  var destination = String(e.destination || '').trim();
  Logger.write('CallAlerting to=' + destination + ' from=' + e.callerid);

  var digits = toDigits(destination);
  if (digits === SHARED_INBOUND_DID) {
    ringSharedInbound(e.call, e.callerid, e.displayName || e.callerid);
    return;
  }
  var inboundUserName = DID_TO_USER[digits];
  if (inboundUserName) {
    // callUser (не Direct): медиа через облако Vox — нужна для записи.
    Logger.write('Inbound DID ' + digits + ' → callUser ' + inboundUserName);
    var inboundUser = VoxEngine.callUser(inboundUserName, e.callerid, {
      displayName: e.displayName || e.callerid,
    });
    enableCallRecording(e.call, inboundUser);
    VoxEngine.easyProcess(e.call, inboundUser);
    return;
  }

  var kind = classifyDestination(destination);
  if (kind === 'user') {
    var userCall = VoxEngine.callUserDirect(e.call, destination, {
      displayName: e.displayName,
      callerid: e.callerid,
      headers: e.headers,
    });
    // P2P — запись через облако недоступна
    VoxEngine.easyProcess(e.call, userCall, function () {}, true);
    return;
  }

  if (kind === 'city') {
    var agent = shortUser(e.callerid);
    var callerid = cliForUser(agent);
    var dest = toE164(destination);
    Logger.write('City call agent=' + agent + ' to=' + dest + ' cli=' + callerid);
    var outbound = VoxEngine.callPSTN(dest, callerid);
    enableCallRecording(e.call, outbound);
    VoxEngine.easyProcess(e.call, outbound);
    return;
  }

  if (kind === 'sip') {
    var sipAgent = shortUser(e.callerid);
    var sipCli = cliForUser(sipAgent);
    Logger.write('SIP dial: ' + destination + ' cli=' + sipCli);
    var sipCall = VoxEngine.callSIP(normalizeSip(destination), sipCli);
    enableCallRecording(e.call, sipCall);
    VoxEngine.easyProcess(e.call, sipCall);
    return;
  }

  Logger.write('Unknown destination, hangup');
  e.call.reject();
  VoxEngine.terminate();
}

if (typeof VoxEngine !== 'undefined') {
  VoxEngine.addEventListener(AppEvents.CallAlerting, onCallAlerting);
}

function sharedRingAgents() {
  var seen = {};
  var list = [];
  for (var did in DID_TO_USER) {
    var name = DID_TO_USER[did];
    if (!name || seen[name]) continue;
    seen[name] = true;
    list.push(name);
  }
  return list;
}

function ringSharedInbound(inboundCall, callerId, displayName) {
  var agents = sharedRingAgents();
  Logger.write('Shared DID ' + SHARED_INBOUND_DID + ' ring ' + agents.length);
  if (!inboundCall || !agents.length) {
    try {
      if (inboundCall) inboundCall.reject();
    } catch (e) {
      /* ignore */
    }
    VoxEngine.terminate();
    return;
  }
  var winner = null;
  var left = agents.length;
  var outs = [];

  function oneDown() {
    if (winner) return;
    left -= 1;
    if (left > 0) return;
    Logger.write('Shared DID: nobody answered');
    try {
      inboundCall.reject();
    } catch (e) {
      /* ignore */
    }
    VoxEngine.terminate();
  }

  for (var i = 0; i < agents.length; i++) {
    (function (name) {
      var out = null;
      var down = false;
      function markDown() {
        if (down || winner) return;
        down = true;
        oneDown();
      }
      try {
        out = VoxEngine.callUser(name, callerId, {
          displayName: displayName || callerId || '',
        });
      } catch (err) {
        Logger.write('Shared callUser ' + name + ': ' + err);
        markDown();
        return;
      }
      outs.push(out);
      out.addEventListener(CallEvents.Connected, function () {
        if (winner) {
          try {
            out.hangup();
          } catch (e) {
            /* ignore */
          }
          return;
        }
        winner = out;
        down = true;
        Logger.write('Shared DID answered by ' + name);
        for (var n = 0; n < outs.length; n++) {
          if (outs[n] === out) continue;
          try {
            outs[n].hangup();
          } catch (e2) {
            /* ignore */
          }
        }
        enableCallRecording(inboundCall, out, true);
        try {
          inboundCall.answer();
        } catch (e3) {
          /* ignore */
        }
        try {
          VoxEngine.sendMediaBetween(inboundCall, out);
        } catch (e4) {
          Logger.write('sendMediaBetween: ' + e4);
        }
        VoxEngine.easyProcess(inboundCall, out);
      });
      out.addEventListener(CallEvents.Failed, markDown);
      out.addEventListener(CallEvents.Disconnected, function () {
        if (winner === out) return;
        markDown();
      });
    })(agents[i]);
  }
}

function cliForUser(username) {
  var u = String(username || '');
  if (OUTBOUND_CLI[u]) return toE164(OUTBOUND_CLI[u]);
  for (var did in DID_TO_USER) {
    if (DID_TO_USER[did] === u) return toE164(did);
  }
  return toE164(DEFAULT_OUTBOUND_CALLER_ID);
}

function shortUser(callerid) {
  var s = String(callerid || '').trim();
  if (!s) return '';
  var at = s.indexOf('@');
  if (at > 0) return s.slice(0, at);
  return s;
}

function classifyDestination(destination) {
  var d = String(destination || '').trim();
  if (!d) return 'empty';
  if (isSip(d)) return 'sip';
  if (isPhoneNumber(d)) return 'city';
  return 'user';
}

function isPhoneNumber(value) {
  var digits = String(value || '').replace(/[^\d+]/g, '');
  return /^\+?\d{8,15}$/.test(digits);
}

function isSip(value) {
  var v = String(value || '').toLowerCase();
  return v.indexOf('@') !== -1 || v.indexOf('sip:') === 0;
}

function toDigits(value) {
  var n = String(value || '').replace(/[^\d]/g, '');
  if (/^8\d{10}$/.test(n)) n = '7' + n.slice(1);
  return n;
}

function toE164(value) {
  var n = toDigits(value);
  return n ? '+' + n : value;
}

function normalizeSip(value) {
  var v = String(value);
  return v.toLowerCase().indexOf('sip:') === 0 ? v : 'sip:' + v;
}
