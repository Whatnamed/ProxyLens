import React from 'react';
import { causalChain, egressNode, intermediateHops, topPolicyGroup } from '../domain/chains';
import { ConnectionRecord } from '../api/types';

/**
 * Renders one connection as the causal chain that PRODUCT.md puts at the centre of the
 * product:  who → where → why → through what → how much.
 *
 * The rendering rule that matters most here is that a MISSING hop is never silently
 * dropped. If the process, the host or the rule was not observed, the chain shows an
 * explicit "未观测" node in that position. Removing the node would produce a chain that
 * reads as a complete explanation when it is not one — the exact failure mode
 * PRODUCT.md 3.4 forbids.
 */

interface Node {
  key: string;
  label: string;
  value: string;
  missing: boolean;
  missingNote?: string;
  tone?: 'target' | 'rule' | 'egress' | 'neutral';
}

export function causalNodes(conn: ConnectionRecord): Node[] {
  const meta = conn.metadata ?? {};
  const chain = causalChain(conn.chains);

  const target = meta.host || meta.sniffHost || meta.destinationIP || '';
  const targetMissing = !target;

  const ruleText = conn.rule
    ? conn.rulePayload
      ? `${conn.rule},${conn.rulePayload}`
      : conn.rule
    : '';

  const policy = topPolicyGroup(conn.chains);
  const mids = intermediateHops(conn.chains);
  const egress = egressNode(conn.chains);

  const nodes: Node[] = [
    {
      key: 'process',
      label: '进程',
      value: meta.process || '未识别进程',
      missing: !meta.process,
      missingNote: '内核未提供进程名（MissingProcess）。TUN 转发或底层驱动流量常出现此情况。',
      tone: 'neutral',
    },
    {
      key: 'target',
      label: '目标',
      value: targetMissing ? '无域名 / 无 IP' : target,
      missing: targetMissing,
      missingNote: 'Host、SniffHost 与 DestinationIP 均为空，无法确定流量去向。',
      tone: 'target',
    },
    {
      key: 'rule',
      label: '命中规则',
      value: ruleText || '未记录规则',
      missing: !ruleText,
      missingNote: '内核未回报命中规则（MissingRule），无法从本记录解释分流依据。',
      tone: 'rule',
    },
  ];

  // Policy group the rule pointed at, then intermediate selectors, then the physical node.
  nodes.push({
    key: 'policy',
    label: '分流策略组',
    value: policy || '未记录',
    missing: !policy,
    missingNote: '缺少代理链记录（MissingChain），无法还原策略组路径。',
    tone: 'neutral',
  });

  mids.forEach((hop, i) => {
    nodes.push({
      key: `mid-${i}`,
      label: '级联选择',
      value: hop,
      missing: false,
      tone: 'neutral',
    });
  });

  nodes.push({
    key: 'egress',
    label: '出站节点',
    value: egress || '未知',
    missing: !egress,
    missingNote: '无法确定最终出站节点。',
    tone: 'egress',
  });

  // Chain length can legitimately be 1 (egress only); guard against double-rendering it.
  if (chain.length === 1 && nodes.some((n) => n.key === 'egress')) {
    return nodes.filter((n) => n.key !== 'policy' || policy !== egress);
  }

  return nodes;
}

export const CausalChain: React.FC<{ conn: ConnectionRecord }> = ({ conn }) => {
  const nodes = causalNodes(conn);
  return (
    <ol className="pl-chain">
      {nodes.map((node, i) => (
        <li key={node.key} className="pl-chain__item">
          <div className="pl-chain__node-wrap">
            <div
              className={`pl-chain__node pl-chain__node--${node.tone ?? 'neutral'} ${
                node.missing ? 'is-missing' : ''
              }`}
              title={node.missing ? node.missingNote : node.value}
            >
              <span className="pl-chain__label">{node.label}</span>
              <span className="pl-chain__value">{node.value}</span>
              {node.missing && <span className="pl-chain__missing">未观测</span>}
            </div>
          </div>
          {i < nodes.length - 1 && (
            <span className="pl-chain__arrow" aria-hidden="true">
              →
            </span>
          )}
        </li>
      ))}
    </ol>
  );
};
