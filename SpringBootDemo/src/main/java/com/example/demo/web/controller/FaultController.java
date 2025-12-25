package com.example.demo.web.controller;

import com.alibaba.fastjson.JSONObject;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import java.util.ArrayList;
import java.util.List;
import java.util.regex.Pattern;

/**
 * 更直观的“故障/异常”接口：
 * - /slow：CPU 密集（正则回溯热点），便于在火焰图中定位到 checkEmail
 * - /alloc：内存分配压力（模拟“吃内存/泄漏感”），便于在 Profiles 里观察内存相关指标
 */
@RestController
public class FaultController {

    private static final Logger logger = LoggerFactory.getLogger(FaultController.class);

    /**
     * 一个写得不合理的邮箱校验正则（容易产生回溯开销）。
     * 注意：Java 的 regex 引擎是回溯型的，确实可能因为输入构造导致 CPU 开销明显。
     */
    private static final Pattern EMAIL_PATTERN = Pattern.compile(
            "^(\\w+([-.][A-Za-z0-9]+)*){3,18}@\\w+([-.][A-Za-z0-9]+)*\\.\\w+([-.][A-Za-z0-9]+)*$"
    );

    /**
     * 构造一个“坏输入”，用于放大正则匹配成本。
     */
    private static final String SLOW_EMAIL_SAMPLE =
            "rosamariachoccelahuaararanda70@gmail.comnnbbb.bbNG.bbb.n¿.?n";

    /**
     * 模拟“泄漏感”：把分配出来的 byte[] 放到静态列表里，短期内不会被 GC 回收。
     * 为了避免把容器直接打爆，做一个上限保护。
     */
    private static final List<byte[]> ALLOC_HOLDER = new ArrayList<>();
    private static final int ALLOC_HOLDER_MAX_BYTES = 256 * 1024 * 1024; // 256MB 上限保护
    private static int allocHolderBytes = 0;

    /**
     * CPU 慢接口：反复触发正则匹配，制造可观测的 CPU 热点。
     *
     * @param loops 重复次数（默认 5000）
     */
    @GetMapping("/slow")
    public JSONObject slow(
            @RequestParam(value = "loops", required = false, defaultValue = "5000") int loops
    ) {
        long startNs = System.nanoTime();
        boolean matched = checkEmail(loops);
        long costMs = (System.nanoTime() - startNs) / 1_000_000;

        JSONObject resp = new JSONObject();
        resp.put("ok", true);
        resp.put("matched", matched);
        resp.put("loops", loops);
        resp.put("cost_ms", costMs);

        logger.info("slow done: loops={}, cost_ms={}, matched={}", loops, costMs, matched);
        return resp;
    }

    /**
     * 按“时长”烧 CPU：比 loops 更稳定，更容易在图上形成尖刺。
     *
     * @param ms 持续烧 CPU 的时长（默认 3000ms）
     */
    @GetMapping("/cpu")
    public JSONObject cpu(
            @RequestParam(value = "ms", required = false, defaultValue = "3000") int ms
    ) {
        long startNs = System.nanoTime();
        long untilNs = startNs + (long) Math.max(1, ms) * 1_000_000L;

        // 做点“不可轻易优化掉”的计算
        long acc = 0;
        while (System.nanoTime() < untilNs) {
            // 反复触发 regex + 一点整数运算，确保 CPU 时间集中在用户态
            if (EMAIL_PATTERN.matcher(SLOW_EMAIL_SAMPLE).matches()) {
                acc++;
            }
            acc += (acc << 1) ^ 0x9e3779b97f4a7c15L;
        }

        long costMs = (System.nanoTime() - startNs) / 1_000_000;
        JSONObject resp = new JSONObject();
        resp.put("ok", true);
        resp.put("ms", ms);
        resp.put("cost_ms", costMs);
        resp.put("acc", acc);
        logger.info("cpu done: ms={}, cost_ms={}, acc={}", ms, costMs, acc);
        return resp;
    }

    /**
     * 内存压力接口：分配 mb 大小的内存块，并按需“持有”引用（更像泄漏）。
     *
     * @param mb   本次分配大小（默认 50MB）
     * @param hold 是否持有引用（默认 true；false 会立即丢弃引用，更像短时分配压力）
     * @param clear 是否清空历史持有（默认 false）
     */
    @GetMapping("/alloc")
    public JSONObject alloc(
            @RequestParam(value = "mb", required = false, defaultValue = "50") int mb,
            @RequestParam(value = "hold", required = false, defaultValue = "true") boolean hold,
            @RequestParam(value = "clear", required = false, defaultValue = "false") boolean clear
    ) {
        if (clear) {
            synchronized (ALLOC_HOLDER) {
                ALLOC_HOLDER.clear();
                allocHolderBytes = 0;
            }
        }

        int bytes = Math.max(1, mb) * 1024 * 1024;
        long startNs = System.nanoTime();
        int actuallyAllocated = allocateMemoryBurst(bytes, hold);
        long costMs = (System.nanoTime() - startNs) / 1_000_000;

        JSONObject resp = new JSONObject();
        resp.put("ok", true);
        resp.put("requested_mb", mb);
        resp.put("allocated_bytes", actuallyAllocated);
        resp.put("hold", hold);
        resp.put("holder_bytes", allocHolderBytes);
        resp.put("cost_ms", costMs);

        logger.info("alloc done: requested_mb={}, allocated_bytes={}, hold={}, holder_bytes={}, cost_ms={}",
                mb, actuallyAllocated, hold, allocHolderBytes, costMs);
        return resp;
    }

    /**
     * 反复正则匹配，用于制造 CPU 热点，方便火焰图观察到 checkEmail。
     */
    private boolean checkEmail(int loops) {
        boolean matched = false;
        int n = Math.max(1, loops);
        for (int i = 0; i < n; i++) {
            if (EMAIL_PATTERN.matcher(SLOW_EMAIL_SAMPLE).matches()) {
                matched = true;
            }
        }
        return matched;
    }

    /**
     * 分配内存并可选持有引用。
     *
     * @return 实际分配的字节数（可能因为上限保护而小于请求值）
     */
    private int allocateMemoryBurst(int bytes, boolean hold) {
        byte[] block = new byte[bytes];
        // 写一点内容，避免被某些优化“看作没用”
        for (int i = 0; i < block.length; i += 4096) {
            block[i] = 1;
        }

        if (!hold) {
            return bytes;
        }

        synchronized (ALLOC_HOLDER) {
            int newTotal = allocHolderBytes + bytes;
            if (newTotal > ALLOC_HOLDER_MAX_BYTES) {
                // 超过上限就不再持有，避免把容器打爆
                return 0;
            }
            ALLOC_HOLDER.add(block);
            allocHolderBytes = newTotal;
            return bytes;
        }
    }
}

